package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/haoxin/boxfleet/internal/model"
	"github.com/sethvargo/go-retry"
)

type networkInventoryState struct {
	Revision int64 `json:"revision"`
}

type networkRequestError struct{ status int }

func (e networkRequestError) Error() string {
	return fmt.Sprintf("network inventory API returned %d", e.status)
}

func (a *Agent) networkRequest(ctx context.Context, method string, report *model.NetworkSnapshot) (int64, error) {
	var body io.Reader
	if report != nil {
		data, err := json.Marshal(report)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.Config.ServerURL, "/")+"/api/node/network-snapshot", body)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Authorization", "Bearer "+a.Config.Token)
	request.Header.Set("X-BoxFleet-Node", a.nodeName())
	request.Header.Set("Content-Type", "application/json")
	response, err := a.client().Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return 0, networkRequestError{response.StatusCode}
	}
	var ack model.NetworkSnapshotAck
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&ack); err != nil {
		return 0, err
	}
	if ack.Revision < 0 {
		return 0, fmt.Errorf("invalid server network revision")
	}
	return ack.Revision, nil
}

func sortLocalAddresses(addresses []model.NodeAddress) {
	sort.Slice(addresses, func(i, j int) bool {
		a, b := addresses[i], addresses[j]
		if a.InterfaceIndex != b.InterfaceIndex {
			return a.InterfaceIndex < b.InterfaceIndex
		}
		if a.Address != b.Address {
			return a.Address < b.Address
		}
		return a.PrefixLength < b.PrefixLength
	})
}

// Network inventory has its own state file and request loop. It never writes
// the traffic/collector state, and no network or filesystem work runs inside a
// netlink notification consumer.
func (a *Agent) runNetworkInventory(ctx context.Context, scan func() ([]model.NodeAddress, error), watch func(context.Context) <-chan struct{}) {
	state := networkInventoryState{}
	path := a.Config.StatePath + ".network"
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	synced := false
	events := watch(ctx)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	var debounce *time.Timer
	var debounceC <-chan time.Time
	defer func() {
		if debounce != nil {
			debounce.Stop()
		}
	}()
	newBackoff := func() retry.Backoff {
		return retry.WithFullJitter(retry.WithCappedDuration(30*time.Second, retry.NewExponential(time.Second)))
	}
	backoff := newBackoff()
	var retryTimer *time.Timer
	var retryC <-chan time.Time
	defer func() {
		if retryTimer != nil {
			retryTimer.Stop()
		}
	}()
	var previous string
	var lastSent time.Time
	refresh := true
	for {
		if refresh {
			refresh = false
			addresses, scanErr := scan()
			sortLocalAddresses(addresses)
			report := model.NetworkSnapshot{Addresses: addresses, Complete: scanErr == nil, CollectedAt: time.Now().UTC().Format(time.RFC3339Nano)}
			if scanErr != nil {
				report.Addresses = []model.NodeAddress{}
				report.Error = scanErr.Error()
				if len(report.Error) > 1024 {
					report.Error = report.Error[:1024]
				}
			}
			if len(addresses) > model.MaxNodeAddresses {
				report.Addresses = []model.NodeAddress{}
				report.Complete = false
				report.Error = "address limit exceeded"
			}
			// A valid address count can still exceed the wire limit. Report the
			// failure instead of retrying an oversized or truncated full snapshot.
			if encoded, _ := json.Marshal(report); len(encoded) > model.MaxNetworkSnapshotBytes-128 {
				report.Addresses = []model.NodeAddress{}
				report.Complete = false
				report.Error = "inventory payload limit exceeded"
			}
			comparison := report
			comparison.CollectedAt = ""
			data, _ := json.Marshal(comparison)
			fingerprint := string(data)
			if fingerprint != previous || time.Since(lastSent) >= 5*time.Minute {
				requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				var err error
				if !synced {
					var current int64
					current, err = a.networkRequest(requestCtx, http.MethodGet, nil)
					if err == nil {
						if current > state.Revision {
							state.Revision = current
						}
						synced = true
					}
				}
				if err == nil {
					if state.Revision >= int64(^uint64(0)>>1)-1 {
						err = fmt.Errorf("network revision exhausted")
					} else {
						state.Revision++
						report.Revision = state.Revision
					}
					if err == nil {
						var saved []byte
						saved, err = json.Marshal(state)
						if err == nil {
							err = atomicWrite(path, saved, 0o600)
						}
					}
					if err == nil {
						var acknowledged int64
						acknowledged, err = a.networkRequest(requestCtx, http.MethodPost, &report)
						if err == nil && acknowledged != report.Revision {
							err = fmt.Errorf("network acknowledgement mismatch")
						}
					}
				}
				cancel()
				if err != nil {
					if status, ok := err.(networkRequestError); ok && status.status == http.StatusConflict {
						synced = false
					}
					if ctx.Err() == nil {
						fmt.Fprintf(os.Stderr, "boxfleet-agent network inventory failed: %v\n", err)
					}
					delay, _ := backoff.Next()
					if retryTimer != nil {
						retryTimer.Stop()
					}
					retryTimer = time.NewTimer(delay)
					retryC = retryTimer.C
				} else {
					previous = fingerprint
					lastSent = time.Now()
					backoff = newBackoff()
					if retryTimer != nil {
						retryTimer.Stop()
					}
					retryC = nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case _, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if debounce == nil {
				debounce = time.NewTimer(2 * time.Second)
			} else {
				debounce.Reset(2 * time.Second)
			}
			debounceC = debounce.C
		case <-debounceC:
			debounceC = nil
			refresh = true
		case <-retryC:
			retryC = nil
			refresh = true
		case <-ticker.C:
			if events == nil {
				events = watch(ctx)
			}
			refresh = true
		}
	}
}
