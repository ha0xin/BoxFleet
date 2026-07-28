// Command singbox-preflight is the Go half of scripts/singbox-preflight.sh.
//
// It exists so the preflight harness exercises the real renderer, the real
// V2Ray stats client and the real journald message shape instead of a shell
// approximation of them:
//
//	render-configs   write sing-box configs produced by internal/server/render
//	query-stats      read counters through internal/v2raystats, as the agent does
//	query-connections verify sing-box 1.14's authenticated connection stream
//	journal-fixture  turn `journalctl -o json` output into log-parser fixture lines
//
// This is off-fleet tooling. Nothing here talks to a BoxFleet server or to a
// production node; render-configs builds a throwaway SQLite database in a
// temporary directory and deletes it again.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/haoxin/boxfleet/internal/secret"
	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/haoxin/boxfleet/internal/server/render"
	"github.com/haoxin/boxfleet/internal/singboxapi"
	"github.com/haoxin/boxfleet/internal/v2raystats"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "render-configs":
		err = renderConfigs(os.Args[2:])
	case "query-stats":
		err = queryStats(os.Args[2:])
	case "query-connections":
		err = queryConnections(os.Args[2:])
	case "journal-fixture":
		err = journalFixture(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		usage()
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "singbox-preflight: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: singbox-preflight <subcommand> [flags]

  render-configs -out DIR [-host HOST] [-mixed-port PORT]
        Write renderer output for a VLESS-Reality node, a Shadowsocks 2022
        node, the disabled-node config and one client config per protocol.

  query-stats [-addr HOST:PORT] [-pattern PATTERN] [-timeout DURATION]
        Print "<counter>\t<value>" for every V2Ray stats counter matching
        PATTERN, using the same client and the same non-resetting read the
        agent performs.

  query-connections -secret-file FILE [-addr HOST:PORT] [-timeout DURATION]
        Subscribe through BoxFleet's singboxapi client and require an initial
        reset plus at least one connection event.

  journal-fixture [-in FILE]
        Read "journalctl -o json" lines and write one MESSAGE per line in the
        format internal/server/db/testdata/singbox_logs/*.input.txt uses.
`)
}

const (
	preflightVLESSNode  = "preflight-vless"
	preflightVLESSProxy = "vless-39090"
	preflightSSNode     = "preflight-ss"
	preflightSSProxy    = "ss-8443"
)

// renderConfigs seeds a throwaway database with the same shapes the renderer
// tests use, then writes real renderer output. Reality keys are generated the
// way the admin API generates them, so `sing-box check` sees a well-formed
// private key rather than the placeholder strings the Go tests get away with.
func renderConfigs(args []string) error {
	flags := flag.NewFlagSet("render-configs", flag.ExitOnError)
	out := flags.String("out", "", "directory to write configs into (required)")
	host := flags.String("host", "127.0.0.1", "public host for the rendered nodes; loopback keeps traffic checks off the network")
	mixedPort := flags.Int("mixed-port", render.DefaultMixedListenPort, "mixed inbound port for the first client config; the second uses this port plus one")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*out) == "" {
		return errors.New("-out is required")
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}

	ctx := context.Background()
	work, err := os.MkdirTemp("", "singbox-preflight-db-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	store, err := db.OpenSQLite(filepath.Join(work, "preflight.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		return err
	}
	if err := seedPreflightFixture(ctx, store, *host); err != nil {
		return err
	}

	nodeVLESS, err := render.RenderNodeConfig(ctx, store, preflightVLESSNode)
	if err != nil {
		return err
	}
	telemetry, err := store.SetNodeConnectionTelemetry(ctx, db.SetNodeConnectionTelemetryParams{
		NodeName: preflightVLESSNode,
		Enabled:  true,
	})
	if err != nil {
		return err
	}
	nodeVLESSTelemetry, err := render.RenderNodeConfig(ctx, store, preflightVLESSNode)
	if err != nil {
		return err
	}
	nodeSS, err := render.RenderNodeConfig(ctx, store, preflightSSNode)
	if err != nil {
		return err
	}
	disabled, err := render.RenderDisabledConfig()
	if err != nil {
		return err
	}
	clientVLESS, err := render.RenderClientConfig(ctx, store, render.ClientConfigParams{
		UserName:        "alice",
		NodeName:        preflightVLESSNode,
		ProxyName:       preflightVLESSProxy,
		MixedListenPort: *mixedPort,
	})
	if err != nil {
		return err
	}
	clientSS, err := render.RenderClientConfig(ctx, store, render.ClientConfigParams{
		UserName:        "alice",
		NodeName:        preflightSSNode,
		ProxyName:       preflightSSProxy,
		MixedListenPort: *mixedPort + 1,
	})
	if err != nil {
		return err
	}

	files := []struct {
		name string
		data []byte
	}{
		{"node-vless-reality.json", nodeVLESS},
		{"node-vless-reality-telemetry.json", nodeVLESSTelemetry},
		{"node-shadowsocks2022.json", nodeSS},
		{"node-disabled.json", disabled},
		{"client-vless-reality.json", clientVLESS},
		{"client-shadowsocks2022.json", clientSS},
	}
	secretPath := filepath.Join(*out, "connection-telemetry-secret.txt")
	if err := os.WriteFile(secretPath, []byte(telemetry.Secret+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Println(secretPath)
	for _, file := range files {
		path := filepath.Join(*out, file.name)
		if err := os.WriteFile(path, append(file.data, '\n'), 0o644); err != nil {
			return err
		}
		fmt.Println(path)
	}

	// The counter check must assert the exact names sing-box was told to
	// account, so read them back out of the rendered config rather than
	// rebuilding "proxy@user" in the shell.
	users, err := statsUsers(nodeVLESS, nodeSS)
	if err != nil {
		return err
	}
	usersPath := filepath.Join(*out, "stats-users.txt")
	if err := os.WriteFile(usersPath, []byte(strings.Join(users, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Println(usersPath)
	return nil
}

func queryConnections(args []string) error {
	flags := flag.NewFlagSet("query-connections", flag.ExitOnError)
	addr := flags.String("addr", "127.0.0.1:9091", "sing-box api service address")
	secretFile := flags.String("secret-file", "", "file containing the api service secret (required)")
	timeout := flags.Duration("timeout", 20*time.Second, "maximum time to wait for a connection event")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*secretFile) == "" {
		return errors.New("-secret-file is required")
	}
	rawSecret, err := os.ReadFile(*secretFile)
	if err != nil {
		return err
	}
	client, err := singboxapi.Dial(singboxapi.Options{
		Address:  *addr,
		Secret:   strings.TrimSpace(string(rawSecret)),
		Interval: time.Second,
	})
	if err != nil {
		return err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	stream, err := client.Subscribe(ctx)
	if err != nil {
		return err
	}
	seenReset := false
	for {
		batch, err := stream.Recv()
		if err != nil {
			return err
		}
		seenReset = seenReset || batch.GetReset_()
		for _, event := range batch.GetEvents() {
			connection := event.GetConnection()
			fmt.Printf("reset=%t type=%s id=%s user=%s destination=%s\n",
				seenReset, event.GetType(), event.GetId(), connection.GetUser(), connection.GetDestination())
			if seenReset {
				return nil
			}
		}
	}
}

func seedPreflightFixture(ctx context.Context, store *db.DB, host string) error {
	keyPair, err := secret.RealityKeyPairX25519()
	if err != nil {
		return err
	}
	for _, name := range []string{"alice", "bob"} {
		if _, err := store.CreateProxyUser(ctx, db.CreateProxyUserParams{Name: name}); err != nil {
			return err
		}
	}
	if _, err := store.CreateNode(ctx, preflightVLESSNode, host, ""); err != nil {
		return err
	}
	settings, err := json.Marshal(map[string]any{
		"server_name":         "www.amazon.com",
		"reality_private_key": keyPair.PrivateKey,
		"reality_public_key":  keyPair.PublicKey,
		"short_id":            "01234567",
		"handshake_server":    "www.amazon.com",
		"handshake_port":      443,
	})
	if err != nil {
		return err
	}
	if _, err := store.CreateProxy(ctx, db.CreateProxyParams{
		NodeName:     preflightVLESSNode,
		Name:         preflightVLESSProxy,
		Protocol:     db.ProtocolVLESSReality,
		Listen:       "0.0.0.0",
		ListenPort:   39090,
		Transport:    db.TransportTCP,
		Enabled:      true,
		SettingsJSON: string(settings),
	}); err != nil {
		return err
	}
	if _, err := store.CreateNode(ctx, preflightSSNode, host, ""); err != nil {
		return err
	}
	if _, err := store.CreateProxy(ctx, db.CreateProxyParams{
		NodeName:     preflightSSNode,
		Name:         preflightSSProxy,
		Protocol:     db.ProtocolShadowsocks2022,
		Listen:       "0.0.0.0",
		ListenPort:   38443,
		Transport:    db.TransportTCPUDP,
		Enabled:      true,
		SettingsJSON: `{}`,
	}); err != nil {
		return err
	}
	// Two users on the VLESS node keep the multi-user inbound shape that
	// production nodes actually run; alice is the one traffic is driven as.
	for _, name := range []string{"alice", "bob"} {
		if _, err := store.BindUserToNode(ctx, name, preflightVLESSNode); err != nil {
			return err
		}
		if _, err := store.IssueVLESSRealityAccess(ctx, db.IssueCredentialParams{
			UserName:  name,
			NodeName:  preflightVLESSNode,
			ProxyName: preflightVLESSProxy,
		}); err != nil {
			return err
		}
	}
	if _, err := store.BindUserToNode(ctx, "alice", preflightSSNode); err != nil {
		return err
	}
	if _, err := store.IssueShadowsocks2022Access(ctx, db.IssueCredentialParams{
		UserName:  "alice",
		NodeName:  preflightSSNode,
		ProxyName: preflightSSProxy,
	}); err != nil {
		return err
	}
	return nil
}

func statsUsers(configs ...[]byte) ([]string, error) {
	var names []string
	seen := make(map[string]bool)
	for _, config := range configs {
		var parsed struct {
			Experimental struct {
				V2RayAPI struct {
					Stats struct {
						Users []string `json:"users"`
					} `json:"stats"`
				} `json:"v2ray_api"`
			} `json:"experimental"`
		}
		if err := json.Unmarshal(config, &parsed); err != nil {
			return nil, err
		}
		for _, name := range parsed.Experimental.V2RayAPI.Stats.Users {
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, errors.New("rendered node configs declared no v2ray_api stats users")
	}
	return names, nil
}

// queryStats mirrors the agent's read exactly: same client, same default
// pattern, and reset=false. Never switch this to reset=true — a lost response
// would then lose traffic (see AGENTS.md).
func queryStats(args []string) error {
	flags := flag.NewFlagSet("query-stats", flag.ExitOnError)
	addr := flags.String("addr", "127.0.0.1:18082", "v2ray_api listen address of the running candidate")
	pattern := flags.String("pattern", "user>>>", "counter name prefix to query")
	timeout := flags.Duration("timeout", 5*time.Second, "query timeout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	stats, err := v2raystats.Query(ctx, *addr, []string{*pattern}, false)
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()
	for _, stat := range stats {
		fmt.Fprintf(writer, "%s\t%d\n", stat.Name, stat.Value)
	}
	return nil
}

// journalFixture converts captured journald JSON into the fixture form that
// internal/server/db/testdata/singbox_logs/*.input.txt uses: one raw sing-box
// MESSAGE per line, with real ESC bytes spelled as the literal characters \x1b
// so the result stays diffable in an editor.
func journalFixture(args []string) error {
	flags := flag.NewFlagSet("journal-fixture", flag.ExitOnError)
	in := flags.String("in", "", "journalctl -o json output to read (default stdin)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	input := os.Stdin
	if strings.TrimSpace(*in) != "" {
		file, err := os.Open(*in)
		if err != nil {
			return err
		}
		defer file.Close()
		input = file
	}
	scanner := bufio.NewScanner(input)
	// journald entries carry no useful length bound; sing-box lines with long
	// hostnames plus the cursor field comfortably exceed the 64 KiB default.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()
	written, skipped := 0, 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry struct {
			Message json.RawMessage `json:"MESSAGE"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil || len(entry.Message) == 0 {
			skipped++
			continue
		}
		message, ok := decodeJournalMessage(entry.Message)
		if !ok {
			skipped++
			continue
		}
		message = strings.TrimSpace(escapeFixtureLine(message))
		// A leading "#" would be read back as a fixture comment and silently
		// dropped, which would look like a parser miss rather than a skip.
		if message == "" || strings.HasPrefix(message, "#") {
			skipped++
			continue
		}
		fmt.Fprintln(writer, message)
		written++
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "journal-fixture: wrote %d lines, skipped %d\n", written, skipped)
	if written == 0 {
		return errors.New("no journal messages captured")
	}
	return nil
}

// decodeJournalMessage handles both journald encodings: a JSON string, and the
// array-of-bytes form journald falls back to for messages it does not consider
// printable UTF-8 — which is exactly what sing-box's ANSI-colored output can
// produce.
func decodeJournalMessage(raw json.RawMessage) (string, bool) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, true
	}
	var octets []byte
	if err := json.Unmarshal(raw, &octets); err == nil {
		return string(octets), true
	}
	return "", false
}

func escapeFixtureLine(message string) string {
	replacer := strings.NewReplacer(
		"\x1b", `\x1b`,
		"\r", " ",
		"\n", " ",
		"\t", " ",
	)
	return replacer.Replace(message)
}
