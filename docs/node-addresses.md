# 节点地址发现

第一阶段代码支持 agent 本机地址清单和 Server 公网来源观测。两者都支持多个 IPv4/IPv6，只保存当前状态，不记录 IP 变化历史；不自动更新客户端入口，不调整 sing-box 出口。

## 本机清单

升级后的 agent 在 daemon 启动时扫描全部接口，Linux 通过 netlink 监听地址和链路变化，去抖 2 秒后重新采集。每 30 秒校准一次，内容变化时上报完整快照，未变化时每 5 分钟重新确认。通知只触发刷新，不用作可靠事件日志；订阅失败时继续轮询并输出诊断，之后重试订阅。

清单包括接口名/索引、前缀、地址族、接口启用状态和作用域。Linux 还报告 temporary、deprecated、tentative、DAD failed。其他平台用标准库轮询，明确标记 flags unavailable。私网、回环和链路本地地址也可查看；`global` 作用域不保证公网可达。

每个报告最多 1024 个地址、256 KiB。枚举中断、地址太多或超过请求大小时上报失败，不发送截断的“完整”清单。Server 保留最近成功地址并显示采集错误；成功的空集合才表示没有地址。10 分钟没有新的成功报告时标为 stale。

采集和发送使用独立 goroutine、有界通知和最多 15 秒的请求上下文，不阻塞配置轮询、计费或日志上传。仅保存最近 revision 到 `<state_path>.network`，复用 renameio 原子写入，不与计费 state 共写，不引入节点数据库。启动时通过 Server 当前 revision 同步；冲突后重新同步。失败重试允许跳过 revision 和合并最新状态，不重放中间地址抖动。

只有成功接受的完整快照会替换当前地址列表；Server 按 revision 防止旧状态覆盖。相同 revision 的相同内容可以幂等重发，不同内容返回 409。采集错误会推进 revision，但不会修改最近成功清单和成功时间。

## 公网来源观测

Server 从认证成功的心跳 HTTP 请求观测来源。已有旧 agent 也能获得公网观测，本机地址清单需要 agent 声明 `network.inventory.v1` 并上报。暂停节点保持此能力；删除节点撤销 token，新注册的同名节点不会继承旧地址。

当前部署通过 Cloudflare Tunnel 访问 bfs。应配置：

```sh
BOXFLEET_CLIENT_IP_SOURCE=cloudflare
BOXFLEET_TRUSTED_PROXY_CIDRS=<cloudflared在bfs看来使用的对端IP>/32
```

对应 CLI 为 `--client-ip-source cloudflare --trusted-proxy-cidrs <CIDR>`，多个 CIDR 用逗号分隔。IPv6 对端使用相应 `/128`。默认 `direct` 仅从 TCP 对端观测，不信任转发头；Cloudflare 模式必须显式指定可信 CIDR，缺少或非法配置会阻止启动。

使用 chi v5.5.0 的 `ClientIPFromHeader`，不使用旧 RealIP，也不覆盖原始 RemoteAddr。Tunnel 接入 Docker 时，可信对端通常是宿主机网桥地址，而非 Cloudflare 公网 IP 段；按实际部署确认，不信任整个私网网段。仅在部署确实由 Cloudflare 覆盖来源头时启用此模式。

正常读取 CF-Connecting-IP；Pseudo IPv4 覆盖模式下读取合法的 CF-Connecting-IPv6。缺头、非法头、网桥/私网或 Pseudo IPv4 本身不会成为公网观测。不可信直接对端不能通过伪造来源头改变观测，公网直接对端可被标记为 direct。Cloudflare Worker 等额外转发可能改变这些头的语义，需另行验证。

公网观测以“节点 + 来源 + IP”保存，不能按地址族仅保留一条，否则多出口会互相覆盖。同一个 IP 的重复请求只刷新观测时间；3 分钟未见显示 stale。节点后续有效观测到达时，删除超过 10 分钟未见的观测，每节点最多保留最近 128 个。节点离线时保留最后状态并标记 stale，不把未出现当成服务商已移除地址。这是有界的当前观测集合，不是历史时间线。

一次心跳只能看到它实际使用的出口，HTTP 连接复用也不会重新选择源地址。列表不是节点全部潜在 NAT 出口的枚举，无法由单次请求补齐另一地址族，更不能证明公网入站可达。第一阶段不调用公共回显服务，不需要服务商端口映射规则。要验证另一地址族或指定源地址，可在后续增加显式主动探测。

## 存储与界面

迁移 `036_node_network.sql` 新增 `node_network_snapshots`、`node_observed_ips`。本机清单是最多 256 KiB 的一个当前 JSON 集合，和 revision、错误、新鲜度一起原子更新；无需再建每次变化的历史行或独立地址历史表。列表查询只读取概要，完整集合由详情端点读取。

- `GET/POST /api/node/network-snapshot`：节点身份从 bearer token 获取；GET 用于 revision 同步，POST 提交快照。
- `GET /api/admin/nodes/{id}/addresses`：当前本机清单、公网观测及采集状态。
- 节点列表的入口地址下方提供地址查看入口；地址对话框将 Published entry、Observed public source、Local addresses 分开显示。

迁移由正常 bfs 启动流程执行。发布前按现有部署规程保留上一版数据库备份；这里没有对生产执行迁移。新增表不会改变旧程序的入口和配置数据，回滚旧二进制时新功能不可用，避免手动删除表或覆盖仍在使用的数据库。

## 验证

普通 Go 测试使用注入扫描器/事件源、httptest 和临时 SQLite，不调用真实 sing-box 或系统服务。覆盖多地址、失败保留、空集合、revision 冲突、可信代理、IPv6/Pseudo IPv4、暂停/删除和同名节点隔离。

Playwright 使用独立临时 DB 和本地测试 bfs，通过回环可信代理模拟 Tunnel 头；覆盖多个 IPv4/IPv6、NAT 私网与公网分开显示、失败保留和桌面/窄屏对话框几何。测试配置不使用生产 DB、端口或服务。

真实节点的地址增删、DAD、订阅溢出及资源压力仍在部署 smoke 中验证；单元测试通过不表示这些场景已在生产演练。
