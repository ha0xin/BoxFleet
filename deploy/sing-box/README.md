# sing-box Deployment Notes

This directory is reserved for generated configuration templates and examples.

BoxFleet should generate complete node configs instead of editing live configs
with string replacement.

## V2Ray API Requirement

BoxFleet's MVP traffic backend uses the `sing-box` V2Ray API. The official
`sing-box` documentation says this API is not included by default; builds must
include the `with_v2ray_api` tag.

GitHub Releases publish a compatible `sing-box-linux-amd64` binary built from
pinned upstream tag `v1.13.13`. Node agents download that file directly during
bootstrap when `sing_box_url` is set.

The current local build tags are:

```text
with_gvisor
with_quic
with_dhcp
with_wireguard
with_utls
with_acme
with_clash_api
badlinkname
tfogo_checklinkname0
with_v2ray_api
```

`with_naive_outbound` is intentionally omitted for the MVP. It pulled in cronet
linking requirements during local testing, and BoxFleet does not need NaiveProxy
outbound for the current VLESS Reality path.

The linker flags should come from the upstream repository:

```bash
cat refs/sing-box/release/LDFLAGS
```

The current local build command is equivalent to:

```bash
cd refs/sing-box
TAGS="with_gvisor,with_quic,with_dhcp,with_wireguard,with_utls,with_acme,with_clash_api,badlinkname,tfogo_checklinkname0,with_v2ray_api"
LDFLAGS="$(cat release/LDFLAGS)"
go build -trimpath -tags "$TAGS" -ldflags "$LDFLAGS" -o ../../dist/deploy/sing-box ./cmd/sing-box
```

Verify the resulting binary with:

```bash
dist/deploy/sing-box version | grep with_v2ray_api
```

## Minimal V2Ray API Check Config

The smoke test validates only that the binary accepts V2Ray API config:

```json
{
  "log": {
    "level": "info"
  },
  "outbounds": [
    {
      "type": "direct",
      "tag": "direct"
    }
  ],
  "route": {
    "final": "direct"
  },
  "experimental": {
    "v2ray_api": {
      "listen": "127.0.0.1:18082",
      "stats": {
        "enabled": true,
        "users": []
      }
    }
  }
}
```

Run:

```bash
sing-box check -c minimal-v2ray-api.json
```

Real generated configs must include user names in
`experimental.v2ray_api.stats.users` so the agent can map V2Ray counters back
to `proxy_accesses.auth_name`.
