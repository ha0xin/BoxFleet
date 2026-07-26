# Service catalog generator

Regenerates `internal/servicecatalog/data/services.tsv`, the pruned
domain→service dataset embedded into `bfs` via `go:embed`.

This generator is run **by a human**, never at build time and never by the
server. The server has no network access to the dataset source and must not
grow one: BoxFleet ships as offline artifacts.

## Source

[`v2fly/domain-list-community`](https://github.com/v2fly/domain-list-community),
MIT licensed. It is the upstream *source* of the `geosite.dat` that both
sing-box and Mihomo consume, so the classification matches the ecosystem
BoxFleet already generates configs for, and its category names are literally
service names.

## Regenerating

From the repository root:

```bash
# Fetch the current upstream master and rewrite the dataset.
go run ./internal/servicecatalog/gen

# Pin a specific upstream ref (recommended for a reproducible bump).
go run ./internal/servicecatalog/gen -ref v2fly-2026-07-01

# Offline: point at an already-extracted checkout's data/ directory.
go run ./internal/servicecatalog/gen -src ../domain-list-community/data
```

Flags: `-src`, `-repo`, `-ref`, `-out`, `-version` (defaults to today's UTC
date, and is what `servicecatalog.Version()` reports).

After regenerating:

```bash
go test ./internal/servicecatalog/...
git diff --stat internal/servicecatalog/data/services.tsv
```

Review the diff size before committing. A bump that moves tens of thousands of
rules usually means an upstream list was renamed or an allowlist entry now
resolves to a different list — check the generator's summary line, which prints
the resolved upstream commit.

## Pruning policy

The full upstream dataset is roughly 5 MB across ~1500 lists. The dataset is
embedded in the server binary, so the generator prunes aggressively. The
current output is **320 KB — 156 services, 18382 rules**. Treat ~500 KB as the
ceiling; past that, drop categories rather than raising it.

1. **Allowlist only.** `allowlist` in `main.go` is the committed policy: one
   entry per upstream list that names a real service or a meaningful functional
   bucket (`ads`, `adult`). Everything else is dropped and falls back to eTLD+1
   at lookup time, which is a perfectly readable answer — add a list only when
   the eTLD+1 is *wrong* or *unhelpful* (a vendor spread across many
   registrable domains, like Apple or Google).
2. **`full:` and bare-domain rules only.** `regexp:` and `keyword:` rules are
   dropped: they need a matcher `servicecatalog` deliberately does not have,
   because per-host regex evaluation over an audit aggregation is not
   affordable. Netflix's `apiproxy-*.amazonaws.com` regexes are the main
   casualty; those hosts classify as `amazonaws.com` via publicsuffix instead.
3. **`include:` is expanded transitively**, so `meta` pulls in `facebook`,
   `instagram`, `whatsapp`, and the rest.
4. **First claim wins.** Allowlist order is significant: a domain is claimed by
   the first entry that yields it. Specific services must precede the umbrella
   list that includes them — `youtube` before `google`, `icloud` before `apple`,
   `github` before `microsoft`. Getting this backwards silently folds a service
   into its parent.
5. **Redundant rules are dropped.** A rule is removed when the nearest ancestor
   rule *across all services* already belongs to the same service, which is
   exactly when the longest-suffix lookup reaches the same answer without it.
   Cross-service ancestors are preserved, so `googlevideo.com` (youtube)
   survives underneath `google.com` (google).
6. **`@attr` tags are stripped, not filtered.** Geographic and functional tags
   (`@cn`, `@ads`) do not affect inclusion.

The generator fails loudly rather than silently degrading:

- an allowlist entry naming a list that no longer exists upstream is a fatal
  error — fix the name instead of deleting the entry;
- an entry that contributes zero rules after pruning is a fatal error, because
  it is fully covered by an earlier entry. Either move it earlier or drop it.
  `origin` (covered by `ea`), `npmjs` (by `github`), and `trello` (by
  `atlassian`) were dropped for this reason.

## Output format

Tab-separated, line-oriented. `#` lines are comments and `!key<TAB>value` lines
are metadata (`version`, `source`, `services`, `rules`). An `S` record starts a
service block and the `D` (suffix) and `F` (exact) rules that follow belong to
it:

```
!version	2026-07-26
S	youtube	YouTube	video
D	youtube.com
F	yt3.googleusercontent.com
```

`D` matches the domain and any subdomain, on label boundaries. `F` matches that
exact host only, and beats any suffix rule.
