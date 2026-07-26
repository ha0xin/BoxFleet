#!/usr/bin/env bash
#
# singbox-preflight.sh — off-fleet gate for moving SING_BOX_REVISION.
#
# BoxFleet pins one sing-box build in .github/workflows/artifacts.yml. Two of the
# four ways a new build can break BoxFleet are SILENT: nothing in the update
# pipeline catches them, because the agent's health check asserts systemd
# ActiveState only. A sing-box that starts cleanly but renamed a build tag zeroes
# traffic billing, and one that reworded a log line sends every network event and
# the whole service audit to zero — with no alert, on every node at once.
#
# This script runs the four checks ADR 0001 requires before the pin moves
# ("Off-fleet checks required before the pin moves past 1.13"):
#
#   1  build tags intact      unattended
#   2  config compatibility   unattended
#   3  log format unchanged   needs a live instance and real traffic (--live)
#   4  traffic counters       needs a live instance and real traffic (--live)
#
# THROWAWAY HOST ONLY. This script never contacts a BoxFleet server and never
# touches a production node: it builds into a temporary directory, runs the
# candidate under a dedicated transient systemd unit, and drives traffic through
# loopback with generated credentials. Do not point it at a fleet node, do not
# run it on the management server, and do not reuse the configs it writes.
#
# See docs/singbox-preflight.md for what each check protects and how to read a
# failure.

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"
workflow="${repo_root}/.github/workflows/artifacts.yml"
fixture_dir="${repo_root}/internal/server/db/testdata/singbox_logs"
candidate_fixture_name="preflight-candidate"

# Live-run defaults. The ports match what the renderer emits, so they are not
# free parameters: v2ray_api is fixed at 127.0.0.1:18082 in render.go and the
# VLESS inbound at 39090 comes from the seeded fixture.
stats_addr="127.0.0.1:18082"
server_port=39090
proxy_port=2080
traffic_target="https://www.cloudflare.com/cdn-cgi/trace"
traffic_requests=5
unit_name="boxfleet-singbox-preflight"

revision=""
tags=""
binary=""
workdir=""
baseline_shape=""
live=0
keep=0

usage() {
	cat <<'EOF'
usage: scripts/singbox-preflight.sh [options] [REVISION]

REVISION defaults to SING_BOX_REVISION in .github/workflows/artifacts.yml.

Options:
  --tags TAGS            build tags (default: SING_BOX_TAGS from artifacts.yml)
  --binary PATH          use an already-built candidate instead of building one
  --workdir DIR          keep artifacts here (default: a fresh mktemp -d)
  --live                 also run checks 3 and 4 against a live instance
  --target URL           traffic target for the live run (default cloudflare trace)
  --requests N           number of proxied requests to drive (default 5)
  --baseline-shape FILE  diff the captured log shape against a previous capture
  --keep                 do not delete the work directory on exit
  -h, --help             show this help

Checks 1 and 2 run unattended. Checks 3 and 4 need a live instance and real
traffic; without --live they are reported NOT RUN, never passed, and the script
exits non-zero because the preflight is incomplete.
EOF
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--tags)
		tags="${2:?--tags needs a value}"
		shift 2
		;;
	--binary)
		binary="${2:?--binary needs a value}"
		shift 2
		;;
	--workdir)
		workdir="${2:?--workdir needs a value}"
		shift 2
		;;
	--live)
		live=1
		shift
		;;
	--target)
		traffic_target="${2:?--target needs a value}"
		shift 2
		;;
	--requests)
		traffic_requests="${2:?--requests needs a value}"
		shift 2
		;;
	--baseline-shape)
		baseline_shape="${2:?--baseline-shape needs a value}"
		shift 2
		;;
	--keep)
		keep=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	--)
		shift
		break
		;;
	-*)
		printf 'unknown option: %s\n\n' "$1" >&2
		usage >&2
		exit 2
		;;
	*)
		if [[ -n "$revision" ]]; then
			printf 'unexpected extra argument: %s\n' "$1" >&2
			exit 2
		fi
		revision="$1"
		shift
		;;
	esac
done
if [[ $# -gt 0 ]]; then
	revision="${revision:-$1}"
fi

# ---------------------------------------------------------------------------
# Output helpers
# ---------------------------------------------------------------------------

status_1="NOT RUN"
status_2="NOT RUN"
status_3="NOT RUN"
status_4="NOT RUN"
detail_1=""
detail_2=""
detail_3=""
detail_4=""

heading() { printf '\n=== %s ===\n' "$*"; }
info() { printf '     %s\n' "$*"; }
todo() { printf '  >  %s\n' "$*"; }

# shout prints a box that is hard to miss in a scrollback. Reserved for the two
# silent failure modes, where the natural reaction — regenerate the golden, bump
# the pin anyway — is the wrong one.
shout() {
	local line
	printf '\n  ############################################################\n'
	for line in "$@"; do
		printf '  #  %s\n' "$line"
	done
	printf '  ############################################################\n\n'
}

record() {
	local check="$1" status="$2" detail="${3:-}"
	case "$check" in
	1)
		status_1="$status"
		detail_1="$detail"
		;;
	2)
		status_2="$status"
		detail_2="$detail"
		;;
	3)
		status_3="$status"
		detail_3="$detail"
		;;
	4)
		status_4="$status"
		detail_4="$detail"
		;;
	esac
	printf '  [%s] check %s%s\n' "$status" "$check" "${detail:+ — $detail}"
}

die() {
	printf '\nsingbox-preflight: %s\n' "$*" >&2
	exit 2
}

require_command() {
	local name
	for name in "$@"; do
		command -v "$name" >/dev/null 2>&1 || die "required command not found: $name"
	done
}

# ---------------------------------------------------------------------------
# Pin and tag list, parsed from the workflow so nothing is spelled twice
# ---------------------------------------------------------------------------

[[ -f "$workflow" ]] || die "workflow not found: $workflow"

workflow_value() {
	sed -n -E "s/^[[:space:]]*$1:[[:space:]]*([^[:space:]#]+).*/\1/p" "$workflow" | head -n 1
}

pinned_revision="$(workflow_value SING_BOX_REVISION)"
[[ -n "$pinned_revision" ]] || die "could not parse SING_BOX_REVISION from $workflow"
revision="${revision:-$pinned_revision}"

if [[ -z "$tags" ]]; then
	tags="$(workflow_value SING_BOX_TAGS)"
fi
[[ -n "$tags" ]] || die "could not parse SING_BOX_TAGS from $workflow"

# The tags CI hard-asserts on are read back out of CI itself. Duplicating the
# list here is exactly the drift this check exists to prevent: if someone adds a
# tag BoxFleet depends on to the workflow's grep block, this harness picks it up
# with no edit.
required_tags="$(grep -oE "grep -F 'with_[a-z0-9_]+'" "$workflow" | sed -E "s/.*'(.*)'/\1/" | sort -u || true)"
[[ -n "$required_tags" ]] || die "could not parse the required build tags out of $workflow (expected \"grep -F 'with_...'\" assertions)"

# ---------------------------------------------------------------------------
# Work directory
# ---------------------------------------------------------------------------

if [[ -z "$workdir" ]]; then
	workdir="$(mktemp -d "${TMPDIR:-/tmp}/singbox-preflight.XXXXXX")"
else
	mkdir -p "$workdir"
	workdir="$(cd -- "$workdir" && pwd)"
fi
config_dir="${workdir}/configs"
capture_dir="${workdir}/capture"
mkdir -p "$config_dir" "$capture_dir"

client_pid=""

# shellcheck disable=SC2329  # invoked by the EXIT trap below
cleanup() {
	local code=$?
	set +e
	if [[ -n "$client_pid" ]] && kill -0 "$client_pid" 2>/dev/null; then
		kill "$client_pid" 2>/dev/null
		wait "$client_pid" 2>/dev/null
	fi
	if [[ $live -eq 1 ]] && command -v systemctl >/dev/null 2>&1; then
		systemctl stop "$unit_name" >/dev/null 2>&1
		systemctl reset-failed "$unit_name" >/dev/null 2>&1
	fi
	# The log replay borrows the repo's fixture directory (see check 3). Leaving
	# a candidate fixture behind would make every later `go test` fail, so remove
	# it whatever happened.
	rm -f "${fixture_dir}/${candidate_fixture_name}.input.txt" \
		"${fixture_dir}/${candidate_fixture_name}.golden.txt"
	if [[ $keep -eq 0 && $code -eq 0 ]]; then
		rm -rf "$workdir"
	fi
	set -e
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Check 1 — build tags intact
# ---------------------------------------------------------------------------

check_build_tags() {
	heading "Check 1 — build tags intact"
	info "revision: ${revision}"
	info "tags:     ${tags}"
	info "required: $(echo "$required_tags" | tr '\n' ' ')"

	local missing_from_tags=""
	local tag
	while read -r tag; do
		[[ -n "$tag" ]] || continue
		case ",${tags}," in
		*",${tag},"*) ;;
		*) missing_from_tags="${missing_from_tags} ${tag}" ;;
		esac
	done <<<"$required_tags"
	if [[ -n "$missing_from_tags" ]]; then
		record 1 FAIL "workflow asserts tags it does not build with:${missing_from_tags}"
		return 1
	fi

	local built_note=""
	if [[ -n "$binary" ]]; then
		[[ -x "$binary" ]] || {
			record 1 FAIL "--binary is not executable: ${binary}"
			return 1
		}
		built_note=" (supplied binary; this harness did not build it)"
		info "using supplied binary: ${binary}"
	else
		require_command go git
		local src="${workdir}/sing-box"
		info "cloning sing-box into ${src}"
		if ! git clone --filter=blob:none https://github.com/SagerNet/sing-box.git "$src" >"${workdir}/clone.log" 2>&1; then
			record 1 FAIL "git clone failed; see ${workdir}/clone.log"
			return 1
		fi
		if ! git -C "$src" checkout "$revision" >>"${workdir}/clone.log" 2>&1; then
			record 1 FAIL "revision ${revision} not found upstream; see ${workdir}/clone.log"
			return 1
		fi
		binary="${workdir}/sing-box-candidate"
		# Identical to the artifacts workflow's build step, including the
		# version ldflag, so the binary under test is the binary CI would ship.
		local version="${revision#v}"
		local ldflags
		ldflags="-X 'github.com/sagernet/sing-box/constant.Version=${version}' $(cat "${src}/release/LDFLAGS") -s -w -buildid="
		info "building at the workflow's tags (this pulls upstream modules)"
		if ! (cd "$src" && CGO_ENABLED=0 go build -trimpath -tags "$tags" -ldflags "$ldflags" -o "$binary" ./cmd/sing-box) >"${workdir}/build.log" 2>&1; then
			record 1 FAIL "build failed; see ${workdir}/build.log"
			return 1
		fi
	fi

	local version_output
	if ! version_output="$("$binary" version 2>&1)"; then
		record 1 FAIL "candidate could not report its version"
		printf '%s\n' "$version_output"
		return 1
	fi
	printf '%s\n' "$version_output" | sed 's/^/     | /'
	printf '%s\n' "$version_output" >"${workdir}/version.txt"

	if ! grep -qF "sing-box version ${revision#v}" <<<"$version_output"; then
		record 1 FAIL "version output does not report ${revision#v}"
		return 1
	fi

	local missing=""
	while read -r tag; do
		[[ -n "$tag" ]] || continue
		grep -qF "$tag" <<<"$version_output" || missing="${missing} ${tag}"
	done <<<"$required_tags"
	if [[ -n "$missing" ]]; then
		shout \
			"BUILD TAG LOST:${missing}" \
			"go build -tags silently ignores unknown tags, so an upstream" \
			"rename compiles clean and drops the feature. with_v2ray_api" \
			"backs per-user traffic billing; with_clash_api backs connection" \
			"tracking. Find the new tag name upstream before bumping the pin."
		record 1 FAIL "tags missing from version output:${missing}"
		return 1
	fi
	record 1 PASS "all required tags present in version output${built_note}"
	return 0
}

# ---------------------------------------------------------------------------
# Check 2 — config compatibility
# ---------------------------------------------------------------------------

check_config_compat() {
	heading "Check 2 — config compatibility"
	require_command go
	info "rendering configs with internal/server/render"
	if ! (cd "$repo_root" && go run ./scripts/singbox-preflight render-configs \
		-out "$config_dir" -host 127.0.0.1 -mixed-port "$proxy_port") >"${workdir}/render.log" 2>&1; then
		record 2 FAIL "renderer failed; see ${workdir}/render.log (CGO is required for the SQLite driver)"
		return 1
	fi
	sed 's/^/     | /' "${workdir}/render.log"

	local failed="" config
	for config in "${config_dir}"/*.json; do
		if "$binary" check -c "$config" >"${workdir}/check-$(basename "$config").log" 2>&1; then
			info "ok   $(basename "$config")"
		else
			info "FAIL $(basename "$config")"
			sed 's/^/     | /' "${workdir}/check-$(basename "$config").log"
			failed="${failed} $(basename "$config")"
		fi
	done
	if [[ -n "$failed" ]]; then
		record 2 FAIL "sing-box check rejected:${failed}"
		return 1
	fi
	record 2 PASS "sing-box check accepted every rendered config"
	return 0
}

# ---------------------------------------------------------------------------
# Live instance — shared by checks 3 and 4
# ---------------------------------------------------------------------------

live_prereqs() {
	local problems=""
	[[ "$(uname -s)" == "Linux" ]] || problems="${problems}\n  - checks 3 and 4 need Linux with systemd/journalctl; this host is $(uname -s)"
	command -v systemd-run >/dev/null 2>&1 || problems="${problems}\n  - systemd-run not found"
	command -v journalctl >/dev/null 2>&1 || problems="${problems}\n  - journalctl not found"
	command -v curl >/dev/null 2>&1 || problems="${problems}\n  - curl not found"
	[[ "$(id -u)" -eq 0 ]] || problems="${problems}\n  - must run as root so the candidate logs to journald under a transient unit"
	if [[ -n "$problems" ]]; then
		printf 'live prerequisites unmet:%b\n' "$problems" >&2
		return 1
	fi
	return 0
}

wait_for_port() {
	local port="$1" attempts=50
	while ((attempts-- > 0)); do
		if (exec 3<>"/dev/tcp/127.0.0.1/${port}") 2>/dev/null; then
			return 0
		fi
		sleep 0.2
	done
	return 1
}

start_live_instance() {
	heading "Live instance"
	systemctl stop "$unit_name" >/dev/null 2>&1 || true
	systemctl reset-failed "$unit_name" >/dev/null 2>&1 || true
	info "starting the candidate as transient unit ${unit_name}"
	if ! systemd-run --unit="$unit_name" --collect --description='BoxFleet sing-box preflight (throwaway)' \
		"$binary" run -c "${config_dir}/node-vless-reality.json" >/dev/null 2>&1; then
		printf 'could not start the transient unit\n' >&2
		return 1
	fi
	if ! wait_for_port "$server_port"; then
		journalctl -u "$unit_name" --no-pager -n 50 >&2 || true
		printf 'candidate never listened on 127.0.0.1:%s\n' "$server_port" >&2
		return 1
	fi
	info "server listening on 127.0.0.1:${server_port}"

	"$binary" run -c "${config_dir}/client-vless-reality.json" >"${workdir}/client.log" 2>&1 &
	client_pid=$!
	if ! wait_for_port "$proxy_port"; then
		sed 's/^/     | /' "${workdir}/client.log" >&2
		printf 'client never listened on 127.0.0.1:%s\n' "$proxy_port" >&2
		return 1
	fi
	info "client mixed inbound on 127.0.0.1:${proxy_port}"
	return 0
}

drive_traffic() {
	local i failures=0
	info "driving ${traffic_requests} proxied requests to ${traffic_target}"
	for ((i = 0; i < traffic_requests; i++)); do
		if ! curl --silent --show-error --max-time 30 --output /dev/null \
			--proxy "http://127.0.0.1:${proxy_port}" "$traffic_target" 2>>"${workdir}/curl.log"; then
			failures=$((failures + 1))
		fi
	done
	if ((failures == traffic_requests)); then
		printf 'every proxied request failed; see %s and %s\n' "${workdir}/curl.log" "${workdir}/client.log" >&2
		return 1
	fi
	if ((failures > 0)); then
		info "${failures}/${traffic_requests} requests failed (see ${workdir}/curl.log)"
	fi
	# sing-box writes the connection log after the request completes, and the
	# journal is not synchronous with it.
	sleep 2
	return 0
}

# ---------------------------------------------------------------------------
# Check 3 — log format unchanged
# ---------------------------------------------------------------------------

# golden_checksums fingerprints the committed goldens so the -update pass below
# cannot quietly rewrite one. Regenerating a golden is how this check gets
# defeated, so the harness refuses to be the thing that does it.
golden_checksums() {
	local file
	for file in "$fixture_dir"/*.golden.txt; do
		if [[ ! -e "$file" || "$(basename "$file")" == "${candidate_fixture_name}.golden.txt" ]]; then
			continue
		fi
		shasum "$file"
	done
}

# log_shape strips values but keeps structure, so two captures taken from two
# different builds are comparable even though hosts, addresses, connection ids
# and timestamps all differ. "action" keeps its value: it is a closed vocabulary
# the parser assigns, and losing one of its three values is exactly the kind of
# drift this comparison is for.
log_shape() {
	sed -E \
		-e 's/^line [0-9]+ /line /' \
		-e 's/=-( |$)/=<empty>\1/g' \
		-e 's/port=0( |$)/port=<empty>\1/g' \
		-e 's/(auth|source|host|port|window_start|window_end|conn)=[^ <][^ ]*/\1=<set>/g' "$1" |
		sort | uniq -c
}

check_log_format() {
	heading "Check 3 — log format unchanged"
	require_command go

	# Phase A: the committed goldens must match the parser before a capture
	# means anything. A failure here is BoxFleet's own regression, not the
	# candidate's.
	info "replaying committed fixtures through parseSingBoxLogEvent"
	if ! (cd "$repo_root" && go test ./internal/server/db -run TestParseSingBoxLogEventGoldenFixtures) >"${workdir}/golden-test.log" 2>&1; then
		sed 's/^/     | /' "${workdir}/golden-test.log"
		record 3 FAIL "committed log-parser goldens already fail; fix that before evaluating a candidate"
		return 1
	fi
	info "committed goldens pass"

	if [[ $live -eq 0 ]]; then
		record 3 "NOT RUN" "needs a live instance and real traffic"
		todo "Re-run on a throwaway Linux host with systemd, as root:"
		todo "    sudo scripts/singbox-preflight.sh --live ${revision}"
		todo "The live run starts the candidate under a transient unit, drives"
		todo "real traffic through it, captures the journal and replays it"
		todo "through TestParseSingBoxLogEventGoldenFixtures."
		return 0
	fi

	local before after
	before="$(golden_checksums)"

	info "capturing the candidate's journal"
	journalctl -u "$unit_name" --no-pager -o json >"${capture_dir}/journal.json" 2>/dev/null || true
	if [[ ! -s "${capture_dir}/journal.json" ]]; then
		record 3 FAIL "journalctl returned nothing for unit ${unit_name}"
		return 1
	fi
	if ! (cd "$repo_root" && go run ./scripts/singbox-preflight journal-fixture \
		-in "${capture_dir}/journal.json") >"${capture_dir}/${candidate_fixture_name}.input.txt" 2>"${workdir}/journal-fixture.log"; then
		sed 's/^/     | /' "${workdir}/journal-fixture.log"
		record 3 FAIL "could not convert the journal capture into fixture form"
		return 1
	fi
	sed 's/^/     | /' "${workdir}/journal-fixture.log"

	if [[ -e "${fixture_dir}/${candidate_fixture_name}.input.txt" ]]; then
		record 3 FAIL "stale ${candidate_fixture_name}.input.txt in ${fixture_dir}; remove it and re-run"
		return 1
	fi
	# The parser is unexported, so the only way to replay a capture through the
	# real thing is as a fixture in its own package. The file is removed again by
	# the EXIT trap, and the committed goldens are checksummed around the call.
	cp "${capture_dir}/${candidate_fixture_name}.input.txt" "${fixture_dir}/${candidate_fixture_name}.input.txt"
	if ! (cd "$repo_root" && go test ./internal/server/db -run TestParseSingBoxLogEventGoldenFixtures \
		-update-singbox-log-golden) >"${workdir}/candidate-golden.log" 2>&1; then
		sed 's/^/     | /' "${workdir}/candidate-golden.log"
		record 3 FAIL "replaying the capture through the parser failed"
		return 1
	fi
	after="$(golden_checksums)"
	if [[ "$before" != "$after" ]]; then
		shout \
			"COMMITTED GOLDENS CHANGED DURING THE CAPTURE REPLAY." \
			"That must not happen and this harness will not leave it in place." \
			"Inspect git diff ${fixture_dir} before doing anything else."
		record 3 FAIL "the replay rewrote committed goldens"
		return 1
	fi
	cp "${fixture_dir}/${candidate_fixture_name}.golden.txt" "${capture_dir}/${candidate_fixture_name}.golden.txt"
	rm -f "${fixture_dir}/${candidate_fixture_name}.input.txt" "${fixture_dir}/${candidate_fixture_name}.golden.txt"

	local parsed_connects tracked_sources
	parsed_connects="$(grep -c 'parsed action=connect ' "${capture_dir}/${candidate_fixture_name}.golden.txt" || true)"
	tracked_sources="$(grep -c '^line [0-9]* tracked-source ' "${capture_dir}/${candidate_fixture_name}.golden.txt" || true)"
	info "parsed connect rows: ${parsed_connects}; tracked sources: ${tracked_sources}"

	log_shape "${capture_dir}/${candidate_fixture_name}.golden.txt" >"${capture_dir}/${candidate_fixture_name}.shape.txt"
	info "capture:  ${capture_dir}/${candidate_fixture_name}.input.txt"
	info "golden:   ${capture_dir}/${candidate_fixture_name}.golden.txt"
	info "shape:    ${capture_dir}/${candidate_fixture_name}.shape.txt"

	if ((parsed_connects == 0)); then
		shout \
			"LOG FORMAT CHANGED: real traffic produced ZERO parsed events." \
			"Nothing else will report this. The agent's health check asserts" \
			"systemd ActiveState only, so this candidate would run green on" \
			"every node while network events and the service audit read zero." \
			"This is a real regression: fix the parser regexes in" \
			"internal/server/db/log_events.go and add a fixture for the new" \
			"wording. NEVER regenerate a golden to make it agree."
		record 3 FAIL "no connect events parsed from real traffic"
		return 1
	fi
	if ((tracked_sources == 0)); then
		shout \
			"SOURCE-IP CORRELATION BROKEN: connections parsed, but no" \
			"'inbound connection from' line was tracked, so every event would" \
			"land with an empty source_ip. Check the first regex in" \
			"internal/server/db/log_events.go against the capture above."
		record 3 FAIL "no connection sources tracked from real traffic"
		return 1
	fi

	if [[ -n "$baseline_shape" ]]; then
		if [[ ! -f "$baseline_shape" ]]; then
			record 3 FAIL "--baseline-shape file not found: ${baseline_shape}"
			return 1
		fi
		if diff -u "$baseline_shape" "${capture_dir}/${candidate_fixture_name}.shape.txt" >"${workdir}/shape.diff"; then
			info "capture shape identical to the baseline"
		else
			sed 's/^/     | /' "${workdir}/shape.diff"
			shout \
				"CAPTURE SHAPE DIFFERS FROM THE PINNED BUILD'S CAPTURE." \
				"Some line kinds are parsed differently than before. Read the" \
				"diff above line by line before accepting this candidate."
			record 3 FAIL "capture shape differs from ${baseline_shape}"
			return 1
		fi
	else
		todo "Keep ${capture_dir}/${candidate_fixture_name}.shape.txt. Capture the"
		todo "same shape from the currently pinned build and pass it as"
		todo "--baseline-shape to diff the two builds directly."
	fi

	shout \
		"Check 3 passed, but read the capture anyway." \
		"It proves the parser still matches SOME lines, not that no line" \
		"kind was lost. A golden diff is always a real regression to" \
		"investigate, never something to regenerate away (ADR 0001)."
	record 3 PASS "${parsed_connects} connect events and ${tracked_sources} tracked sources parsed from live traffic"
	return 0
}

# ---------------------------------------------------------------------------
# Check 4 — traffic counters intact
# ---------------------------------------------------------------------------

counter_value() {
	local file="$1" name="$2" value
	# A counter absent from a snapshot reads as zero: sing-box creates counters
	# lazily on the first routed connection, so the "before" snapshot legitimately
	# has none of them.
	if [[ ! -f "$file" ]]; then
		printf '0'
		return 0
	fi
	value="$(awk -F'\t' -v want="$name" '$1 == want { print $2; exit }' "$file")"
	printf '%s' "${value:-0}"
}

check_traffic_counters() {
	heading "Check 4 — traffic counters intact"
	if [[ $live -eq 0 ]]; then
		record 4 "NOT RUN" "needs a live instance and real traffic"
		todo "Re-run on a throwaway Linux host with systemd, as root:"
		todo "    sudo scripts/singbox-preflight.sh --live ${revision}"
		todo "The live run snapshots user>>>NAME>>>traffic>>>{uplink,downlink}"
		todo "before and after real traffic and asserts both increment."
		return 0
	fi

	local user="vless-39090@alice"
	local uplink="user>>>${user}>>>traffic>>>uplink"
	local downlink="user>>>${user}>>>traffic>>>downlink"
	local before_uplink before_downlink after_uplink after_downlink
	before_uplink="$(counter_value "${capture_dir}/stats-before.txt" "$uplink")"
	before_downlink="$(counter_value "${capture_dir}/stats-before.txt" "$downlink")"
	after_uplink="$(counter_value "${capture_dir}/stats-after.txt" "$uplink")"
	after_downlink="$(counter_value "${capture_dir}/stats-after.txt" "$downlink")"

	sed 's/^/     | /' "${capture_dir}/stats-after.txt"

	# Counters are created lazily on the first routed connection
	# (loadOrCreateCounter in experimental/v2rayapi/stats.go), so an absent
	# counter after real traffic is the failure, not an absent counter before it.
	if ! grep -qF "$uplink" "${capture_dir}/stats-after.txt" || ! grep -qF "$downlink" "${capture_dir}/stats-after.txt"; then
		shout \
			"TRAFFIC COUNTERS MISSING after real traffic." \
			"Expected ${uplink}" \
			"     and ${downlink}." \
			"The agent reads exactly these names; if they moved, every node" \
			"reports zero bytes and per-user billing silently stops. Check" \
			"the counter naming in experimental/v2rayapi/stats.go upstream."
		record 4 FAIL "expected counters absent for ${user}"
		return 1
	fi
	if ((after_uplink <= before_uplink)) || ((after_downlink <= before_downlink)); then
		shout \
			"TRAFFIC COUNTERS DID NOT INCREMENT." \
			"uplink   ${before_uplink} -> ${after_uplink}" \
			"downlink ${before_downlink} -> ${after_downlink}" \
			"The counters exist but stopped accounting. Same consequence as" \
			"losing them: zero bytes on every node, no alert."
		record 4 FAIL "counters did not increase (uplink ${before_uplink}->${after_uplink}, downlink ${before_downlink}->${after_downlink})"
		return 1
	fi
	record 4 PASS "uplink ${before_uplink}->${after_uplink}, downlink ${before_downlink}->${after_downlink}"
	return 0
}

# ---------------------------------------------------------------------------
# Run
# ---------------------------------------------------------------------------

printf 'sing-box preflight — THROWAWAY HOST ONLY, never a production node\n'
printf 'candidate revision: %s (pinned: %s)\n' "$revision" "$pinned_revision"
printf 'work directory:     %s\n' "$workdir"

failures=0
check_build_tags || failures=$((failures + 1))

if [[ "$status_1" == "PASS" ]]; then
	check_config_compat || failures=$((failures + 1))
else
	record 2 "NOT RUN" "check 1 must pass first; there is no candidate binary to check with"
fi

live_ready=0
if [[ $live -eq 1 ]]; then
	if [[ "$status_2" != "PASS" ]]; then
		printf '\n--live requested but checks 1-2 did not pass; not starting an instance\n' >&2
	elif ! live_prereqs; then
		printf '\n--live requested but this host cannot host the live run (see above)\n' >&2
	elif ! start_live_instance; then
		printf '\n--live requested but the candidate could not be started\n' >&2
	else
		(cd "$repo_root" && go run ./scripts/singbox-preflight query-stats -addr "$stats_addr") \
			>"${capture_dir}/stats-before.txt" 2>"${workdir}/stats-before.log" || true
		if drive_traffic; then
			if (cd "$repo_root" && go run ./scripts/singbox-preflight query-stats -addr "$stats_addr") \
				>"${capture_dir}/stats-after.txt" 2>"${workdir}/stats-after.log"; then
				live_ready=1
			else
				sed 's/^/     | /' "${workdir}/stats-after.log" >&2
				printf '\nv2ray stats query failed against %s\n' "$stats_addr" >&2
			fi
		fi
	fi
	if [[ $live_ready -eq 0 ]]; then
		# --live was asked for and could not be delivered. Failing is the only
		# honest outcome: reporting NOT RUN here would read as "try again", when
		# what happened is that the candidate could not be exercised at all.
		record 3 FAIL "live run requested but could not be completed"
		record 4 FAIL "live run requested but could not be completed"
		failures=$((failures + 2))
	fi
fi

if [[ $live -eq 0 || $live_ready -eq 1 ]]; then
	check_log_format || failures=$((failures + 1))
	check_traffic_counters || failures=$((failures + 1))
fi

heading "Summary"
printf '  check 1  build tags intact       [%s]%s\n' "$status_1" "${detail_1:+ — $detail_1}"
printf '  check 2  config compatibility    [%s]%s\n' "$status_2" "${detail_2:+ — $detail_2}"
printf '  check 3  log format unchanged    [%s]%s\n' "$status_3" "${detail_3:+ — $detail_3}"
printf '  check 4  traffic counters        [%s]%s\n' "$status_4" "${detail_4:+ — $detail_4}"

if [[ $keep -eq 1 || $failures -gt 0 ]]; then
	printf '\nartifacts kept in %s\n' "$workdir"
	keep=1
fi

if ((failures > 0)); then
	printf '\nPREFLIGHT FAILED — do not move SING_BOX_REVISION to %s.\n' "$revision"
	exit 1
fi
if [[ "$status_3" != "PASS" || "$status_4" != "PASS" ]]; then
	printf '\nPREFLIGHT INCOMPLETE — checks 3 and 4 have not been run against %s.\n' "$revision"
	printf 'A check that did not run is not a check that passed. Finish them on a\n'
	printf 'throwaway host before moving the pin. See docs/singbox-preflight.md.\n'
	exit 2
fi
printf '\nPREFLIGHT PASSED for %s. Moving the pin is now an ordinary release change.\n' "$revision"
exit 0
