# sing-box Build

BoxFleet pins sing-box in `.github/workflows/artifacts.yml` and publishes a
Linux amd64 binary with the node release. `SING_BOX_REVISION` changes only after
the new upstream version passes config, traffic, update, and rollback tests.

The required build tag is `with_v2ray_api`; without it the agent cannot read
per-user traffic counters. The workflow also enables the networking features
listed in `SING_BOX_TAGS`. `with_naive_outbound` is intentionally omitted
because the supported VLESS-Reality path does not need it and it introduces
additional linker requirements.

Verify a candidate with:

```bash
sing-box version | grep with_v2ray_api
sing-box check -c <generated-config.json>
```

Generated configs expose V2Ray stats only on `127.0.0.1:18082` and enumerate
every rendered access `auth_name`. Counter naming is:

```text
user>>>AUTH_NAME>>>traffic>>>uplink
user>>>AUTH_NAME>>>traffic>>>downlink
```

The agent maps these counters back to access rows. Config details belong in
[configuration rendering](../../docs/config-generation.md); release and install
steps belong in [deployment](../../docs/deployment.md).
