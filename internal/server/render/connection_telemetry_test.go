package render

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haoxin/boxfleet/internal/server/db"
)

// These tests guard the one change on this branch that could take down the
// production fleet. Nodes run sing-box 1.13, which does not parse the 1.14
// `services` block at all, so a node that has not opted in must render exactly
// the bytes it rendered before the block existed — not "equivalent JSON", the
// same bytes, because the node hashes the config and `sing-box check` either
// accepts the file or the node refuses the update.

const (
	// Placeholders substituted into the golden comparison. The credential UUID
	// is minted per test run and the telemetry secret is 32 random bytes, so
	// neither can be pinned; everything around them can.
	goldenUUIDPlaceholder   = "00000000-0000-0000-0000-000000000000"
	goldenSecretPlaceholder = "0000000000000000000000000000000000000000000000000000000000000000"
)

// TestRenderNodeConfigDefaultShapeIsUnchanged pins the bytes a node with no
// opt-in receives. A diff here is the fleet-breaking regression: investigate
// it, do not regenerate the golden.
func TestRenderNodeConfigDefaultShapeIsUnchanged(t *testing.T) {
	ctx := context.Background()
	store := openRenderTestDB(t)
	seedVLESSRealityFixture(t, ctx, store)

	config, err := RenderNodeConfig(ctx, store, "azus")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "services") {
		t.Fatalf("default node config mentions services:\n%s", config)
	}
	assertGolden(t, "node_config_default.golden.json", redactRendered(t, ctx, store, config, ""))
}

// TestRenderNodeConfigOptedInEmitsAPIService pins the opted-in shape against
// sing-box 1.14's option.APIServiceOptions: `services[0]` is a `type: api`
// entry carrying only listen, listen_port and secret.
func TestRenderNodeConfigOptedInEmitsAPIService(t *testing.T) {
	ctx := context.Background()
	store := openRenderTestDB(t)
	seedVLESSRealityFixture(t, ctx, store)

	telemetry, err := store.SetNodeConnectionTelemetry(ctx, db.SetNodeConnectionTelemetryParams{
		NodeName: "azus",
		Enabled:  true,
	})
	if err != nil {
		t.Fatal(err)
	}

	config, err := RenderNodeConfig(ctx, store, "azus")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "node_config_telemetry.golden.json", redactRendered(t, ctx, store, config, telemetry.Secret))

	// The golden is redacted, so assert separately that the real secret — not a
	// placeholder, not an empty string — reached the config. An empty secret
	// disables auth entirely in sing-box's daemon authenticate().
	var parsed struct {
		Services []struct {
			Type       string `json:"type"`
			Tag        string `json:"tag"`
			Listen     string `json:"listen"`
			ListenPort int64  `json:"listen_port"`
			Secret     string `json:"secret"`
		} `json:"services"`
	}
	if err := json.Unmarshal(config, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Services) != 1 {
		t.Fatalf("services = %d, want 1", len(parsed.Services))
	}
	service := parsed.Services[0]
	if service.Type != "api" || service.Tag != connectionTelemetryServiceTag {
		t.Fatalf("unexpected service identity: %+v", service)
	}
	if service.Listen != db.DefaultConnectionTelemetryListenAddress || service.ListenPort != db.DefaultConnectionTelemetryListenPort {
		t.Fatalf("unexpected service endpoint: %+v", service)
	}
	if service.Secret != telemetry.Secret {
		t.Fatalf("service secret = %q, want the stored secret", service.Secret)
	}
	if len(service.Secret) < db.MinConnectionTelemetrySecretLength {
		t.Fatalf("service secret is %d characters, want at least %d", len(service.Secret), db.MinConnectionTelemetrySecretLength)
	}
}

// TestRenderNodeConfigOptOutRestoresDefaultBytes is the rollback path: an
// operator who turns telemetry off gets the 1.13-compatible config back
// verbatim, with no residue from the opt-in.
func TestRenderNodeConfigOptOutRestoresDefaultBytes(t *testing.T) {
	ctx := context.Background()
	store := openRenderTestDB(t)
	seedVLESSRealityFixture(t, ctx, store)

	before, err := RenderNodeConfig(ctx, store, "azus")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetNodeConnectionTelemetry(ctx, db.SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	enabled, err := RenderNodeConfig(ctx, store, "azus")
	if err != nil {
		t.Fatal(err)
	}
	if string(enabled) == string(before) {
		t.Fatal("opting in did not change the rendered config")
	}
	if _, err := store.SetNodeConnectionTelemetry(ctx, db.SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	after, err := RenderNodeConfig(ctx, store, "azus")
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("opting out did not restore the default bytes:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// A second node in the same database must be unaffected by the first node's
// opt-in. This is the blast-radius check for a per-node flag: getting the
// lookup wrong (fleet-wide instead of per-node) would push an unparseable
// config to every 1.13 node at once.
func TestRenderNodeConfigOptInIsPerNode(t *testing.T) {
	ctx := context.Background()
	store := openRenderTestDB(t)
	seedVLESSRealityFixture(t, ctx, store)
	seedShadowsocks2022Fixture(t, ctx, store, "bob")

	if _, err := store.SetNodeConnectionTelemetry(ctx, db.SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	other, err := RenderNodeConfig(ctx, store, "ss-node")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(other), "services") {
		t.Fatalf("opting azus in leaked a services block onto ss-node:\n%s", other)
	}
}

// A paused node is told to stop serving. It must never carry the telemetry
// block, whatever its opt-in says: the config it receives has to parse on 1.13
// as well, and a stopped sing-box has nothing to report.
func TestRenderDisabledConfigNeverCarriesTelemetry(t *testing.T) {
	config, err := RenderDisabledConfig()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "services") {
		t.Fatalf("disabled config mentions services:\n%s", config)
	}
}

// The renderer fails closed rather than emitting an endpoint that upstream's
// authenticate() would leave unauthenticated, or one bound off-loopback. The
// facade and the schema both refuse these values on write; this is the layer
// that still holds if a row is edited by hand.
func TestConnectionTelemetryRenderFailsClosed(t *testing.T) {
	ctx := context.Background()
	store, raw := openRenderTelemetryTestDB(t)
	seedVLESSRealityFixture(t, ctx, store)
	if _, err := store.SetNodeConnectionTelemetry(ctx, db.SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	node, err := store.GetNode(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}

	// Only listen_address is reachable this way: the port range and the
	// 32-character secret floor are CHECK constraints, so raw SQL cannot get
	// past them either. The renderer still validates both — see
	// TestConnectionTelemetryValidators in internal/server/db for those
	// branches — but the schema is the layer that makes them unreachable.
	hostile := map[string]string{
		"public bind":   `UPDATE node_connection_telemetry SET listen_address = '0.0.0.0' WHERE node_id = ?`,
		"hostname bind": `UPDATE node_connection_telemetry SET listen_address = 'localhost' WHERE node_id = ?`,
	}
	for name, statement := range hostile {
		if _, err := raw.ExecContext(ctx, statement, node.ID); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := RenderNodeConfig(ctx, store, "azus"); err == nil {
			t.Errorf("%s: render succeeded, want a refusal", name)
		}
		if _, err := raw.ExecContext(ctx,
			`UPDATE node_connection_telemetry SET listen_address = ?, listen_port = ? WHERE node_id = ?`,
			db.DefaultConnectionTelemetryListenAddress, db.DefaultConnectionTelemetryListenPort, node.ID); err != nil {
			t.Fatal(err)
		}
	}
}

// openRenderTelemetryTestDB returns the facade plus a raw handle on the same
// file. The raw handle exists to write rows the facade and the schema both
// refuse, which is the only way to reach the renderer's own fail-closed checks.
func openRenderTelemetryTestDB(t *testing.T) (*db.DB, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "boxfleet.db")
	store, err := db.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := raw.Close(); err != nil {
			t.Error(err)
		}
	})
	return store, raw
}

// redactRendered replaces the two per-run values in a rendered node config with
// fixed placeholders so the rest of the bytes can be pinned exactly. Both
// values are substituted textually, not re-marshalled, because re-marshalling
// through a map would sort the keys and stop the golden from proving field
// order.
func redactRendered(t *testing.T, ctx context.Context, store *db.DB, config []byte, telemetrySecret string) []byte {
	t.Helper()
	access, err := store.GetProxyCredential(ctx, "alice", "azus", "vless-39090")
	if err != nil {
		t.Fatal(err)
	}
	var credential db.VLESSRealityCredential
	if err := json.Unmarshal([]byte(access.CredentialJSON), &credential); err != nil {
		t.Fatal(err)
	}
	redacted := strings.ReplaceAll(string(config), credential.UUID, goldenUUIDPlaceholder)
	if telemetrySecret != "" {
		redacted = strings.ReplaceAll(redacted, telemetrySecret, goldenSecretPlaceholder)
	}
	return []byte(redacted)
}

func assertGolden(t *testing.T, name string, actual []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\nactual output was:\n%s", path, err, actual)
	}
	// json.MarshalIndent emits no trailing newline; the golden files keep one
	// so they are ordinary text files. Nothing else is normalised — the point
	// of these fixtures is that every other byte matches.
	expected := strings.TrimRight(string(raw), "\n")
	if string(actual) != expected {
		t.Fatalf("rendered config does not match %s.\nA diff here is a real regression — investigate it, do not regenerate.\nwant:\n%s\ngot:\n%s", path, expected, actual)
	}
}
