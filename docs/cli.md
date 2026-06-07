# CLI Draft

The management CLI is `bf`. It talks to the local or remote BoxFleet server.

The node binary is `boxfleet-agent` and only exposes local maintenance commands.

## Admin CLI

### Database

```text
bf db init
bf db migrate
bf db status
```

### Server

```text
bf server run
bf server check
```

### Users

```text
bf user create <name>
bf user list
bf user show <name>
bf user disable <name>
bf user enable <name>
bf user set-quota <name> <bytes|gb>
bf user set-multiplier <name> <ratio>
bf user set-expire <name> <date>
bf user node-info <name>
bf user node-info <name> --node <node>
bf user node-info <name> --format json
```

### Nodes

```text
bf node create <name> --host <host>
bf node list
bf node show <name>
bf node disable <name>
bf node enable <name>
bf node token issue <name>
bf node bootstrap <name> --ssh <ssh-target> --server-url <url> --agent-bin <path> --sing-box-url <url>
```

The Web UI's Nodes page also has a simplified Add Node flow. Enter a node name,
copy the generated `boxfleet-bootstrap:...` string, download `boxfleet-agent`
on the node, then run:

```bash
sudo ./boxfleet-agent bootstrap 'boxfleet-bootstrap:...'
```

The bootstrap command writes the agent config, installs the current binary to
the configured agent path, checks that `sing-box` is built with
`with_v2ray_api` (or downloads it when `sing_box_url` is present), installs
systemd units, applies config, and starts the agent service.

### Proxies

```text
bf proxy create vless-reality --node <node> --port 443 --sni <name>
bf proxy create ss2022 --node <node> --port 8388 --method 2022-blake3-aes-128-gcm
bf proxy create hy2 --node <node> --port 8443 --cert-path <path> --key-path <path> --up-mbps 100 --down-mbps 100
bf proxy list --node <node>
bf proxy show <name> --node <node>
bf proxy disable <name> --node <node>
bf proxy enable <name> --node <node>
```

`bf proxy create` does not ask for transport. BoxFleet derives it from the
protocol: VLESS Reality uses TCP, Hysteria2 uses UDP, and Shadowsocks 2022 uses
TCP+UDP. The stored transport is shown in lists because listener conflict
validation depends on it.

The current tested render/apply path is VLESS Reality. Shadowsocks 2022 and
Hysteria2 can be represented as proxy rows, but full access issuing, rendering,
and client information still need implementation.

### Access

```text
bf access issue <user> --node <node> --proxy <proxy>
bf access show <user> --node <node> --proxy <proxy>
```

### User Node Binding

```text
bf bind user <user> --node <node>
bf bind disable <user> --node <node>
bf bind enable <user> --node <node>
bf bind set-quota <user> --node <node> <bytes|gb>
bf bind set-multiplier <user> --node <node> <ratio>
bf bind set-route <user> --node <node> <route-profile>
```

### Config

```text
bf config render --node <node>
bf config render-client <user> --node <node> --proxy <proxy>
bf config diff --node <node>
bf config publish --node <node>
bf config status --node <node>
bf config rollback --node <node>
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
bf stats node <node>
bf stats top users
bf stats top nodes
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
