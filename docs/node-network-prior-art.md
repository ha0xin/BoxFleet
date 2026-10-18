# 节点地址与出口策略：开源复用调研

日期：2026-10-18。对应 [实施计划](node-addresses-and-egress-plan.md)。调研时检查了现有依赖、候选项目的固定提交、相关源码和许可证，并运行 Linux 只读采集试验。后续已按此选型实现第一阶段，当前行为和验证见 [节点地址发现](node-addresses.md)；生产未部署。

## 1. 结论与最小新增依赖

第一阶段完整包含 agent 本机地址发现和 Server 公网观测，只保存当前状态，不保存 IP 变化历史。建议新增 **`github.com/vishvananda/netlink` 一个直接依赖**，优先验证已发布的 `v1.3.1`；来源 IP 解析优先升级已有 chi 至 `v5.5.0` 复用其实现。其他平台先用标准库轮询。源地址 HTTP 探测按需使用标准库，出口规则实施整体放在第二阶段。

sing-box 继续作为独立进程执行路由和拨号，不嵌入它的引擎；BoxFleet 只编译自身有限的业务规则并生成配置。Tailscale、Blackbox Exporter 用于参考故障处理和测试场景，不整体引入 agent。自动 DNS 更新尚未决定，`libdns` 留作后续候选。

“一个新增直接依赖”不等于没有间接依赖：netlink v1.3.1 依赖 `vishvananda/netns` 和 `x/sys`；后者项目已有。实际合并前仍应检查 module graph、二进制变化和运行资源，本次没有测量完整 agent 的体积或 RSS。

## 2. 按能力选择复用对象

| 能力 | 建议复用 | 方式与结论 |
| --- | --- | --- |
| Linux 本机地址、IPv6 状态 | `vishvananda/netlink` | 新增直接依赖，优先候选；不用 shell 解析 `ip addr` |
| 非 Linux 的地址清单 | 标准库 `net.Interfaces` / `Interface.Addrs` | 轮询退路；不能声称拥有 Linux 相同的地址状态字段 |
| IP/CIDR 解析与规范化 | 标准库 `net/netip` | 直接使用；无需额外 IP 工具库 |
| 可信代理来源 IP | 已有 chi，拟升级 v5.5.0 | 复用 `ClientIPFromHeader` 及可信对端 CIDR；不自行实现 XFF 解析 |
| 指定源地址的 HTTP 探测 | 标准库 `net.Dialer` / `net/http` | 实现薄业务封装；参考 Blackbox 的探测语义 |
| 重试、退避、取消 | 已有 `sethvargo/go-retry` | 复用现有指数退避、full jitter 和 context |
| 小型状态原子替换 | 已有 `google/renameio/v2` | 复用现有 `writeFileAtomic`，不引入 WAL/数据库 |
| DNS、路由匹配、实际拨号 | 发布 pin 对应的 sing-box 二进制 | 复用其实现，通过配置控制；不在 BoxFleet 重写规则执行器 |
| 用户授权、策略与版本发布 | 仓库现有 DB、renderer、发布/回退代码 | 补齐已有表与 facade，不引入另一套代理控制面 |
| 前端规则排序及表单 | 已有 Kumo、TanStack、react-hook-form/zod、dnd-kit | 沿用现有库和页面模式，不引入另一个规则编辑器框架 |
| 未来域名跟随 | `libdns/libdns` + 一个选定 provider | 后续候选；不在首期安装，不把 DNS API 密钥发给节点 |

## 3. 地址采集库比较

### 3.1 首选：vishvananda/netlink

检查了 [v1.3.1](https://github.com/vishvananda/netlink/tree/17daef607c6442d47b0565343cf8a69f985a4cb7) 和一个更新的 main 快照；推荐先使用发布版，不能将 main 特有字段当成 v1.3.1 的契约。

已确认的复用点：

- [`AddrList`](https://github.com/vishvananda/netlink/blob/17daef607c6442d47b0565343cf8a69f985a4cb7/addr_linux.go) 获取地址清单；返回的 `Addr` 包含地址、LinkIndex、Flags、Scope、Preferred/Valid lifetime 等信息。
- 同文件 `AddrSubscribeWithOptions` 订阅 IPv4/IPv6 地址事件，提供 `ListExisting`、错误回调和接收缓冲配置。
- [`addr.go`](https://github.com/vishvananda/netlink/blob/17daef607c6442d47b0565343cf8a69f985a4cb7/addr.go) 的字段可以支持 tentative、deprecated、temporary 等状态解释；标志常量使用已有 `golang.org/x/sys/unix`。
- [`go.mod`](https://github.com/vishvananda/netlink/blob/17daef607c6442d47b0565343cf8a69f985a4cb7/go.mod) 较简单；该发布版声明 Go 1.12，不要求升级 BoxFleet 的 Go 基线。许可证为 [Apache-2.0](https://github.com/vishvananda/netlink/blob/17daef607c6442d47b0565343cf8a69f985a4cb7/LICENSE)。

建议把通知当作“需要重新采集”的提示，不把它当作可靠事件账本。持续 drain 事件通道，合并成一个 dirty 标记；采集、HTTP 上报和磁盘保存不在消费循环里执行。先订阅、后全量采集，期间若有事件再采一次；仍保留周期校准。

源码揭示的边界：

1. `AddrList` 返回 `ErrDumpInterrupted` 时列表可能不完整，必须重试；不能用这个结果确认地址消失。
2. 订阅通过阻塞的 `ch <- AddrUpdate` 发送。不能认为关闭 done 就一定能退出被阻塞的发送；调用者需持续 drain 到库关闭输出通道，再等待退出。该生命周期需要专门验证。
3. 收包错误会结束订阅；错误回调应记录诊断、触发重新订阅和完整扫描。缓冲溢出不能用扩大缓冲无限掩盖。
4. 普通地址事件不覆盖所有网卡、链路和路由变化。增加 Link 通知可以更快发现接口变化；只有地址清单未变也不代表出口路由健康。
5. 只监听当前网络命名空间。首期不进入其他 netns，也不调用修改网卡、路由或地址的 API。
6. 地址 lifetime 倒计时不能直接参与快照 hash，否则每次采集都可能产生假变化。使用规范化状态；只在状态或有效期限有实质变化时更新变更记录。

首次部署验证正常用户及现有 service 约束下的订阅权限；失败时退化为轮询并显示原因，不为只读采集直接增加 `CAP_NET_ADMIN` 或强制接收缓冲权限。

### 3.2 备选：jsimonetti/rtnetlink

[`rtnetlink/v2`](https://github.com/jsimonetti/rtnetlink/tree/9498c7a8aec537cfc4efb4ff7e817540415c9069) 提供 `Address.List`、类型化地址消息、`JoinGroup` / `Receive`，也暴露 flags 和 cache lifetime；许可证为 [MIT](https://github.com/jsimonetti/rtnetlink/blob/9498c7a8aec537cfc4efb4ff7e817540415c9069/LICENSE.md)。

这是可行备选，但需要自己组合订阅、消息过滤和重连。检查的 master 已经是 `/v2`，不能照旧文章 import 无版本路径。该快照的 go.mod 还声明 `cilium/ebpf`、更高版本的 `x/sys`、`x/net`、`x/sync` 等；module graph 影响和实际链接成本要分开评估，不能据此断言全部代码都会进二进制。

相比之下，vishvananda 的高层地址订阅 API 更接近本期需求。首期只选一个，不同时引入两套 netlink 实现；如首选的关闭/重连行为无法满足要求，再对发布版 rtnetlink 做同样 PoC。

### 3.3 参考工作：Tailscale netmon

检查的 [netmon.go](https://github.com/tailscale/tailscale/blob/e3c3ef18a51a2d70c902c28f465939888464fb86/net/netmon/netmon.go) 和 [Linux 实现](https://github.com/tailscale/tailscale/blob/e3c3ef18a51a2d70c902c28f465939888464fb86/net/netmon/netmon_linux.go) 值得参考：网络事件去抖、重新获取状态、重复通知抑制，以及平台不支持 netlink 组时退化轮询。

不直接 import `tailscale.com/net/netmon`：检查的版本依赖其 eventbus、logger、metrics 等项目内包，根模块要求 Go 1.27.2，高于当前 BoxFleet go.mod 的 1.26.3。它也服务于移动网络切换，抽象范围比节点地址清单更广。所查文件和根许可证为 BSD-3-Clause；需要复制代码时逐文件保留相应声明，本方案先参考设计，不复制实现。

## 4. 出口探测：优先复用控制连接，按需补充主动探测

默认由自家 Server 在节点认证后观测请求来源，省去公共回显请求。2026-10-18 的当前生产 Tunnel 链路检查已确认四个用户指定的 NAT 节点请求携带 CF-Connecting-IP，也能观察到其他节点的 IPv6 来源；实现时需补可信代理配置和来源保存。详见实施计划第 3.3 节。单次 HTTP 请求不能枚举节点的全部出口，指定源地址与另一地址族仍需下面的主动探测。

### 4.0 来源 IP 已有库，无需重写解析器

当前项目 chi v5.2.5 的 `middleware.RealIP` 不检查可信对端，且不读取 CF-Connecting-IP，不适合直接用于本功能。已核对发布版 [chi v5.5.0 的 client_ip.go](https://github.com/go-chi/chi/blob/v5.5.0/middleware/client_ip.go)：`ClientIPFromHeader(header, trustedIPPrefixes...)` 支持 CF 单 IP 头、可信对端 CIDR、IPv4-mapped IPv6 规范化及 context 读取；不覆盖原始 RemoteAddr。建议升级现有依赖并做路由回归，而非另加一个来源解析库。

BoxFleet 保留的薄适配仅负责选择直连/Cloudflare 模式、配置实际 Tunnel 直连对端、在启用 Pseudo IPv4 覆盖模式时选择 CF-Connecting-IPv6，以及筛选不可作为公网观测的本地/保留地址。来源解析本身交给 chi；不按 Cloudflare 公网网段误判本机 cloudflared 的 Docker 网桥对端。

另一个可行候选是 [realclientip-go](https://github.com/realclientip/realclientip-go)，0BSD、无外部依赖，提供 SingleIPHeaderStrategy 等策略。检查的固定提交 `545b12e0e8c797c02889d850f5bfd2d2738c7048` 还包含 TrustedPeerStrategy，但已发布的 v1.0.0 没有该构造器，不能混用版本 API。首选 chi 已覆盖需求，暂不新增此库。无论使用哪个库，仍需要明确部署中的可信代理边界。

### 4.1 直接使用的 API

使用 [`net.Dialer.LocalAddr`](https://pkg.go.dev/net#Dialer) 固定源地址，并在 DialContext 强制 `tcp4` / `tcp6`，由 [`http.Transport`](https://pkg.go.dev/net/http#Transport) 完成 TLS/HTTP。响应中的 IP 用 [`netip.ParseAddr`](https://pkg.go.dev/net/netip#ParseAddr) 校验和规范化。

每个绑定地址使用明确关联的 transport；限制活跃及空闲连接数量，地址撤销时关闭旧空闲连接。探测客户端与携带 node token 的控制面客户端分开，不能把认证 header 带到回显服务。

需要在薄封装里明确的行为：

- `Proxy = nil`，避免环境中的 HTTP(S)_PROXY 改变被观测出口；禁止重定向或只允许预先定义的目的地。
- 绑定源地址和强制目标地址族同时设置；不允许 IPv6 探测失败后走 IPv4。
- 校验 HTTP 状态、响应大小、IP 地址族和超时，默认不重试格式错误和永久错误。
- 区分“系统默认出口样本”和“指定地址的探测”；不将 DNS 查询所用源地址与 HTTP 连接的源地址混为一谈。
- 依然只有访问某个回显服务的观测，无法枚举所有 NAT 地址，也不证明公网入站可达。

这些是 HTTP API 的业务配置和结果验证，不是重新实现协议客户端。首期无需引入 STUN、ICMP、公共 IP 聚合库或路由守护进程。

### 4.2 参考：Prometheus Blackbox Exporter

检查 [TCP prober](https://github.com/prometheus/blackbox_exporter/blob/d18e9bed33270911c09826234a4e4f500f775087/prober/tcp.go)、[HTTP prober](https://github.com/prometheus/blackbox_exporter/blob/d18e9bed33270911c09826234a4e4f500f775087/prober/http.go) 及 [配置说明](https://github.com/prometheus/blackbox_exporter/blob/d18e9bed33270911c09826234a4e4f500f775087/CONFIGURATION.md)。其 TCP 探测使用 `net.Dialer.LocalAddr`；HTTP 有地址族选择、代理、重定向和 TLS 等处理。

借鉴它对 DNS、连接、TLS 和 HTTP 失败的区分，以及 `ip_protocol_fallback` 的显式语义。注意配置里的 `source_ip_address` 在所查文档属于 TCP/DNS/ICMP 等探测部分，不能直接声称 HTTP 模块具有同名源地址选项。

不引入整个 prober 包或部署完整 exporter 到节点：它围绕 Prometheus registry、探测模块和更多协议组织，依赖范围超过本期需求。所查根许可证为 Apache-2.0。按需源地址公网回显仍由 BoxFleet 的标准库封装完成。

## 5. 规则和配置：复用 sing-box 的执行能力

### 5.1 不引入另一个规则引擎

BoxFleet 的工作是验证有限规则 AST、解析资源 ID、展开 auth names 和生成配置，不是判断实时连接该走哪里。域名、CIDR、端口和 TCP/UDP 的匹配交给 sing-box。

不为此新增 CEL、通用策略引擎或 JavaScript 规则运行时；仓库现有 goja 用于其他功能，也不代表应把出口规则变成可执行脚本。首期也不做通用 sing-box 配置编辑器。

保留少量 typed JSON 结构并以 golden 测试约束输出，沿用当前 renderer。可以参考上游类型和 JSON Schema 来核对字段，但不直接 import 整个 sing-box option 包并假设它是独立、稳定、无副作用的配置 SDK。

### 5.2 优先参考的上游源码

以 `v1.14.2` 对应提交 `af6e64c3b69e6132ebaee0e1a3d24e93903f6709` 为研究基准；实施时重新核对发布 pin。

- [`option/outbound.go`](https://github.com/SagerNet/sing-box/blob/af6e64c3b69e6132ebaee0e1a3d24e93903f6709/option/outbound.go)：resolver 对象、源地址绑定和版本字段。
- [`common/dialer/resolve.go`](https://github.com/SagerNet/sing-box/blob/af6e64c3b69e6132ebaee0e1a3d24e93903f6709/common/dialer/resolve.go)：域名解析与直接 IP 拨号的区别。
- [`route/route.go`](https://github.com/SagerNet/sing-box/blob/af6e64c3b69e6132ebaee0e1a3d24e93903f6709/route/route.go) 和 [`route/rule/rule_default.go`](https://github.com/SagerNet/sing-box/blob/af6e64c3b69e6132ebaee0e1a3d24e93903f6709/route/rule/rule_default.go)：预解析地址、动作与规则验证路径。需在实现阶段继续做行为测试，不把阅读部分源码当作 IPv6-only 已验证。

所查 sing-box 根许可证为 GPL-3.0-or-later。保持现有独立二进制边界，参考公开配置和行为；若以后计划链接或复制其实现，单独核查许可证和分发要求，不把 option 类型文件当作无许可证约束的代码素材。

### 5.3 仓库内已经完成的工作

- `schema/schema.sql` 的 node_outbounds、route_profiles、user_node_bindings 关联列，以及现有 sqlc/goose 流程。
- `internal/server/db/proxy_credentials.go` 与 renderer 中的 auth name、授权筛选、统计用户列表。
- `internal/server/db/config_versions.go` 的 immutable 版本和发布流程，以及 agent 的 check、应用、服务状态验证与 last-good 回退。
- `internal/agent/operations.go` 已采用 go-retry；`agent.go` 的小型状态保存与 `writeFileAtomic` 已使用 renameio。
- 前端已有 dnd-kit，可用于规则排序；表格、表单和请求状态继续走既有组件。Mihomo 编辑器的 schema/Monaco 工作不用于生成服务器端出口规则。

这些组件需要扩展，不代表已有出口策略实际上生效。servicecatalog 也不是路由规则编译库，不能将“分类到某个服务”直接等同于准确、可更新的目标规则集。

## 6. 自动入口更新的后续复用候选

[`libdns/libdns`](https://github.com/libdns/libdns/tree/43599001f01ca904aa17a7f6f5857d0b94691bcd) 提供跨 DNS provider 的记录读写接口，所查版本为 MIT、go.mod 要求 Go 1.23。只有决定做域名跟随时，才在中央服务器引入接口和一个选定 provider，复用 provider 的认证、DNS 记录操作和错误处理。

接口库本身不提供所有供应商实现，也不处理 BoxFleet 的地址选择、去抖、记录归属或发布策略。provider 的版本、许可证和更新语义需另查。更新前比较自己管理的 A/AAAA 记录，不能覆盖同名记录集合中管理员维护的其他地址。

不建议默认另装一个独立 DDNS daemon：它会和 Server 同时拥有“哪个 IP 应发布”的决定权。也不预先把 Cloudflare SDK 绑进节点；DNS TTL、缓存和客户端重连仍是恢复速度的边界。

## 7. 已执行的试验及限制

试验代码保存在 [只读采集 PoC](references/node-network-poc/main.go)，使用独立 Go module，不改变根 go.mod/go.sum。

环境：Linux amd64，Go 1.27.1；候选 `netlink v1.3.1`。执行内容只有地址读取、初始地址事件订阅和正常关闭，没有添加/删除地址或修改路由。

实际结果：全量枚举成功，读取 13 个地址；订阅收到 13 条初始事件；持续消费事件时关闭成功。输出只包含数量，不保存本机地址。

运行方式：

```bash
cd docs/references/node-network-poc
go run .
```

这不是可靠性、跨平台或性能验收。没有触发实际地址变化、缓冲溢出、dump 中断、权限拒绝、网络命名空间切换和 IPv6 tentative 状态，没有验证真实源地址公网探测，也没有在项目声明的 Go 1.26.3 上编译完整 agent。

接入前还应覆盖：订阅失败后的轮询及恢复；风暴下有界队列和 RSS；阻塞消费下关闭；DAD/失效地址过滤；接口删除与索引重用；节点重启后的 revision；HTTP 代理环境变量、重定向、连接池复用、IPv4/IPv6 不回退；旧策略默认输出不变。

## 8. 研究记录与后续步骤

[固定提交清单](references/node-network-prior-art-manifest.json) 记录获取过的源码 URL 与 SHA-256。清单是获取范围，不表示逐行审计所有文件；本文明确讨论的部分才是本次已检查范围。原始参考文件位于本次研究临时目录，未复制到业务代码；正式集成时按固定版本重新获取。

建议下一步按以下顺序实施：

1. 第一阶段验证 netlink 的权限、风暴和错误恢复及 chi 升级；完成 agent 快照上报、Server 可信来源观测和当前状态展示，不保存历史。
2. 第二阶段才验证 sing-box pin 的严格地址族和按用户分流，补齐 node_outbounds / route_profiles 的 CRUD 和 renderer。
3. 按需补充标准库源地址探测；域名跟随和出口组继续作为独立后续设计。

不需要在此之前引入 Tailscale 模块、Blackbox Exporter、通用规则引擎、节点数据库、队列系统或新的前端编辑器。
