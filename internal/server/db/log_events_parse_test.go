package db

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The domain/service audit views read log_events.target_host, which exists only
// because parseSingBoxLogEvent scrapes it out of sing-box's journal text. The
// upstream log format is not a contract, so these fixtures pin the three
// regexes against realistic lines: any upstream wording change shows up here as
// a golden diff instead of as silently empty audit panels.
var updateSingBoxLogGolden = flag.Bool(
	"update-singbox-log-golden",
	false,
	"rewrite internal/server/db/testdata/singbox_logs/*.golden.txt from the current parser output",
)

const singBoxLogFixtureDir = "testdata/singbox_logs"

func TestParseSingBoxLogEventGoldenFixtures(t *testing.T) {
	inputs, err := filepath.Glob(filepath.Join(singBoxLogFixtureDir, "*.input.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) == 0 {
		t.Fatalf("no fixtures under %s", singBoxLogFixtureDir)
	}
	for _, input := range inputs {
		name := strings.TrimSuffix(filepath.Base(input), ".input.txt")
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			got := renderSingBoxLogFixture(string(raw))
			golden := strings.TrimSuffix(input, ".input.txt") + ".golden.txt"
			if *updateSingBoxLogGolden {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (re-run with -update-singbox-log-golden to create it)", err)
			}
			if got != string(want) {
				t.Errorf("parser output drifted for %s\n--- got ---\n%s\n--- want ---\n%s", input, got, want)
			}
		})
	}
}

// renderSingBoxLogFixture replays a fixture through parseSingBoxLogEvent with a
// single shared connectionSources map, exactly as RecordLogEvents does for one
// report, and normalizes the outcome of every line into one text row.
func renderSingBoxLogFixture(input string) string {
	sources := make(map[string]string)
	var out strings.Builder
	for index, raw := range strings.Split(input, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		before := make(map[string]string, len(sources))
		for id, source := range sources {
			before[id] = source
		}
		parsed, ok := parseSingBoxLogEvent(decodeLogFixtureEscapes(line), sources)
		fmt.Fprintf(&out, "line %02d ", index+1)
		switch {
		case ok:
			fmt.Fprintf(
				&out,
				"parsed action=%s auth=%s source=%s host=%s port=%d window_start=%s window_end=%s\n",
				logFixtureField(parsed.Action),
				logFixtureField(parsed.AuthName),
				logFixtureField(parsed.SourceIP),
				logFixtureField(parsed.TargetHost),
				parsed.TargetPort,
				logFixtureField(parsed.WindowStart),
				logFixtureField(parsed.WindowEnd),
			)
		default:
			if tracked := addedConnectionSources(before, sources); len(tracked) > 0 {
				fmt.Fprintf(&out, "tracked-source %s\n", strings.Join(tracked, " "))
			} else {
				out.WriteString("ignored\n")
			}
		}
	}
	return out.String()
}

// Fixtures spell ESC as the literal two characters \x1b so they stay readable
// and diffable in an editor.
func decodeLogFixtureEscapes(line string) string {
	return strings.ReplaceAll(line, `\x1b`, "\x1b")
}

func logFixtureField(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func addedConnectionSources(before, after map[string]string) []string {
	added := make([]string, 0, len(after))
	for id, source := range after {
		if previous, ok := before[id]; ok && previous == source {
			continue
		}
		added = append(added, fmt.Sprintf("conn=%s source=%s", id, source))
	}
	sort.Strings(added)
	return added
}

// TestParseSingBoxLogEventDropsUnattributedActions pins data-model finding #2:
// parseSingBoxLogEvent emits three actions, but "invalid_connection" and
// "outbound_connect" carry no auth_name — invalid connections are rejected
// before authentication and outbound lines are logged by the outbound handler,
// which never sees the inbound user. RecordLogEvents drops both, first on the
// empty AuthName guard and again when the proxy user cannot be resolved, so
// log_events only ever holds action="connect". Grouping by action therefore
// yields exactly one bucket and the admin UI's five-value action filter is
// aspirational; do not build a stacked-by-action chart on top of it.
func TestParseSingBoxLogEventDropsUnattributedActions(t *testing.T) {
	sources := make(map[string]string)
	invalid, ok := parseSingBoxLogEvent(
		"+0000 2026-05-15 18:08:47 \x1b[31mERROR\x1b[0m [\x1b[38;5;38m3583260653\x1b[0m 55ms] inbound/vless[vless-39090]: process connection from 67.230.167.42:52570: TLS handshake: REALITY: processed invalid connection",
		sources,
	)
	if !ok {
		t.Fatal("invalid connection line was not parsed")
	}
	if invalid.Action != "invalid_connection" || invalid.AuthName != "" {
		t.Fatalf("invalid connection parse = %#v", invalid)
	}
	outbound, ok := parseSingBoxLogEvent(
		"+0000 2026-05-16 03:23:44 \x1b[36mINFO\x1b[0m [\x1b[38;5;99m1000000001\x1b[0m 5ms] outbound/direct[direct]: outbound connection to github.com:443",
		sources,
	)
	if !ok {
		t.Fatal("outbound connection line was not parsed")
	}
	if outbound.Action != "outbound_connect" || outbound.AuthName != "" {
		t.Fatalf("outbound connection parse = %#v", outbound)
	}

	ctx := context.Background()
	store := openTestDB(t)
	seedTrafficFixture(t, ctx, store)
	if err := store.RecordLogEvents(ctx, LogEventReport{
		NodeName: "azus",
		Events: []LogEventInput{
			{Action: "sing-box", RawMessage: "+0000 2026-05-16 03:23:43 \x1b[36mINFO\x1b[0m [\x1b[38;5;140m3999106428\x1b[0m 0ms] inbound/vless[vless-39090]: inbound connection from 115.27.221.55:62895"},
			{Action: "sing-box", RawMessage: "+0000 2026-05-16 03:23:43 \x1b[36mINFO\x1b[0m [\x1b[38;5;140m3999106428\x1b[0m 236ms] inbound/vless[vless-39090]: [vless-39090@alice] inbound connection to speed.cloudflare.com:443"},
			{Action: "sing-box", RawMessage: "+0000 2026-05-15 18:08:47 \x1b[31mERROR\x1b[0m [\x1b[38;5;38m3583260653\x1b[0m 55ms] inbound/vless[vless-39090]: process connection from 67.230.167.42:52570: TLS handshake: REALITY: processed invalid connection"},
			{Action: "sing-box", RawMessage: "+0000 2026-05-16 03:23:44 \x1b[36mINFO\x1b[0m [\x1b[38;5;99m1000000001\x1b[0m 5ms] outbound/direct[direct]: outbound connection to github.com:443"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := store.sql.QueryContext(ctx, `SELECT action, COUNT(*) FROM log_events GROUP BY action ORDER BY action`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	actions := map[string]int64{}
	for rows.Next() {
		var action string
		var count int64
		if err := rows.Scan(&action, &count); err != nil {
			t.Fatal(err)
		}
		actions[action] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions["connect"] != 1 {
		t.Fatalf("stored action buckets = %#v, want exactly one \"connect\" bucket", actions)
	}
}

// TestParseSingBoxLogEventPreservesTargetHostCase records why every aggregation
// over target_host has to apply lower(): only aggregate_key lowercases the
// host, so the stored column keeps whatever casing sing-box logged and
// "Example.com" and "example.com" are distinct rows.
func TestParseSingBoxLogEventPreservesTargetHostCase(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedTrafficFixture(t, ctx, store)
	if err := store.RecordLogEvents(ctx, LogEventReport{
		NodeName: "azus",
		Events: []LogEventInput{
			{Action: "sing-box", RawMessage: "+0000 2026-05-16 04:00:00 \x1b[36mINFO\x1b[0m [\x1b[38;5;140m7000000001\x1b[0m 5ms] inbound/vless[vless-39090]: [vless-39090@alice] inbound connection to Example.COM:443"},
			{Action: "sing-box", RawMessage: "+0000 2026-05-16 04:00:01 \x1b[36mINFO\x1b[0m [\x1b[38;5;140m7000000001\x1b[0m 6ms] inbound/vless[vless-39090]: [vless-39090@alice] inbound connection to example.com:443"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListRecentLogEventsByNode(ctx, "azus", 10)
	if err != nil {
		t.Fatal(err)
	}
	hosts := make([]string, 0, len(events))
	for _, event := range events {
		hosts = append(hosts, event.TargetHost)
	}
	sort.Strings(hosts)
	if len(hosts) != 2 || hosts[0] != "Example.COM" || hosts[1] != "example.com" {
		t.Fatalf("stored hosts = %#v, want the original casing preserved on both rows", hosts)
	}
}
