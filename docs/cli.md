# CLI Draft

The management CLI is `bf`. It talks to the local or remote BoxFleet server.

The node binary is `boxfleet-agent` and only exposes local maintenance commands.

## Admin CLI

### Database

```text
bf db init          # alias: bf db migrate
bf db status
```

Interactive day-to-day CRUD (users, nodes, proxies, access) is also available in
the admin Web UI, which is now the primary surface for it. `bf` remains the
local/offline tool for database setup, config render/publish, node enrollment,
and diagnostics.

### Users

```text
bf user create <name>
bf user list
bf user show <name>
bf user disable <name>
bf user enable <name>
bf user delete <name>
bf user set-quota <name> <bytes|gb>
bf user set-expire <name> <date|none>
bf user node-info <name>
bf user node-info <name> --node <node>
bf user node-info <name> --format json
```

### Nodes

```text
bf node create <name> --host <host>
bf node list
bf node show <name>
bf node rename <current-name> <new-name>
bf node disable <name>          # pause: agent stops sing-box, keeps reporting; token intact
bf node enable <name>
bf node delete <name>           # decommission: disable + revoke the node's tokens
bf node token issue <name>
bf node token delete <name>
bf node bootstrap <name> --ssh <ssh-target> --server-url <url> --agent-bin <path> --sing-box-url <url>
```

`bf node delete` is a soft decommission: it sets the node disabled and revokes its
tokens (the record is kept). `bf node disable` is a reversible pause that leaves
the token valid so the node can be re-enabled later. See "Node lifecycle" in
`docs/architecture.md`.

Renaming changes the canonical node name and retains the previous name as an
alias. Existing agent tokens remain valid; the server returns the new canonical
name to the agent, which persists it in `agent.json`.

The Web UI's Nodes page also has a simplified Add Node flow. Enter a node name,
copy the generated command, and run it on the node:

```bash
curl -fsSL https://<server-host>:18081/install.sh -o /tmp/boxfleet-install.sh
sudo sh /tmp/boxfleet-install.sh 'boxfleet-bootstrap:...'
```

The server embeds `install.sh`. That script downloads the versioned
`boxfleet-agent` and `sing-box` release assets, installs them under
`/opt/boxfleet/bin`, and then runs `boxfleet-agent bootstrap`. The bootstrap
command writes the agent config, verifies `sing-box` has `with_v2ray_api`,
installs systemd units, applies config, and starts the agent service.

### Proxies

```text
bf proxy create vless-reality --node <node> --port 443 --sni <name>
bf proxy create ss2022 --node <node> --port 8388 --method 2022-blake3-aes-128-gcm
bf proxy create hy2 --node <node> --port 8443 --cert-path <path> --key-path <path> --up-mbps 100 --down-mbps 100
bf proxy list --node <node>
bf proxy show <name> --node <node>
bf proxy rename <current-name> <new-name> --node <node>
bf proxy set-short-id <name> <short-id> --node <node>
bf proxy disable <name> --node <node>
bf proxy enable <name> --node <node>
bf proxy delete <name> --node <node>
```

`bf proxy create` does not ask for transport. BoxFleet derives it from the
protocol: VLESS Reality uses TCP, Hysteria2 uses UDP, and Shadowsocks 2022 uses
TCP+UDP. The stored transport is shown in lists because listener conflict
validation depends on it.

Proxy names are globally unique because they are also the base Mihomo profile
name. Rename retains the old name as an alias and does not change existing
access UUIDs or `auth_name` values. Reality short IDs are normalized to lowercase
and must be empty or an even-length hexadecimal value of at most 8 characters.

The current tested render/apply path is VLESS Reality. Shadowsocks 2022 and
Hysteria2 can be represented as proxy rows, but full access issuing, rendering,
and client information still need implementation.

### Access

```text
bf access issue <user> --node <node> --proxy <proxy>
bf access show <user> --node <node> --proxy <proxy>
bf access revoke <user> --node <node> --proxy <proxy>
bf access delete <user> --node <node> --proxy <proxy>   # alias of revoke
```

### User Node Binding

```text
bf bind user <user> --node <node>
bf bind list
bf bind disable <user> --node <node>
bf bind enable <user> --node <node>
bf bind delete <user> --node <node>
bf bind set-quota <user> --node <node> <bytes|gb>
bf bind set-multiplier <user> --node <node> <ratio|inherit>
```

### Config

```text
bf config render --node <node>
bf config render-client <user> --node <node> --proxy <proxy>
bf config publish --node <node>
bf config status --node <node>
```

`bf config publish` renders the desired `sing-box` config, stores it as a
versioned config row, and marks it as the node's target version. It does not SSH
to the node. The node agent pulls the target config from the server API and
reports whether the version was applied.

`bf config status` shows target/current config versions, hashes, last apply
status, last apply error, last heartbeat, and reported agent/sing-box versions.

### Stats

```text
bf stats user <name>
bf stats user <name> --node <node>
bf stats v2ray --addr 127.0.0.1:18082 --pattern 'user>>>vless-39090@alice' --format json
```

`bf stats user` reads persisted traffic deltas from the management database.
The agent's first V2Ray API counter read after a fresh state file establishes a
baseline; `bf stats user` shows only deltas uploaded after that baseline.

`bf stats v2ray` is a live diagnostic command for querying a node's local V2Ray
API through SSH forwarding or another private path.

### Logs

```text
bf logs user <name>
bf logs node <node>
```

`bf logs node` reads recent structured events for one node. `bf logs user`
filters to events that the server parser mapped to a known `auth_name`. Both
commands support `--limit`. Raw log rows are not retained in normal operation;
use the structured fields and `raw_message` sample on `log_events` for
diagnostics.

## Node Agent CLI

```text
boxfleet-agent run
boxfleet-agent install
boxfleet-agent check
boxfleet-agent once
boxfleet-agent bootstrap <boxfleet-bootstrap:string>
boxfleet-agent version
```

`boxfleet-agent bootstrap` is the paste-on-node entrypoint. `boxfleet-agent
install` performs the same install/apply/service steps using an existing config
file.

The agent CLI should not include user, quota, node, route, subscription, or
server administration commands.
