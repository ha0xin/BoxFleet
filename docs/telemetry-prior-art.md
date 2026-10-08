# 实时审计传输的 prior art 与复用选择

日期：2026-10-07。已阅读官方文档和以下固定提交的关键源码；未把候选库引入 agent/server，也未部署新传输。许可证按获取的文件核对，不以项目名称代替逐文件审查。

结论：不自行编写 WebSocket framing、WAL 文件格式、分段恢复或重试算法。可以直接依赖小型 Go 库；完整采集平台优先参考实现，或仅部署在中央服务器。没有一个通用库能替 BoxFleet 决定累计快照、用户归属和计费语义。

## 复用清单

| 部分 | 候选与源码 | 如何复用 | 已确认的边界 |
| --- | --- | --- | --- |
| WSS 传输 | [coder/websocket](https://github.com/coder/websocket)，`dial.go`、`accept.go`、`read.go`、`conn.go`，ISC | 首选候选，直接依赖；使用 context、读限制、Ping、关闭握手 | 不提供自动持久化、业务 ACK 或跨重连重放；必须持续读取以处理控制帧 |
| 追加日志存储 | [tidwall/wal](https://github.com/tidwall/wal)，`wal.go`、`wal_test.go`，MIT | Go 库候选；复用有序索引、批量写入、读取和截断 | WAL 不是完整发送队列；容量、ACK 水位、损坏策略和并发发送仍需适配 |
| 文件 FIFO | [nsqio/go-diskqueue](https://github.com/nsqio/go-diskqueue)，`diskqueue.go`、测试，MIT | Go 库候选；单消费者先 Peek，再发送，业务 ACK 后消费 | `ReadChan` 会推进消费；Put 成功不等价于其后异步 fsync 成功，需审计同步错误与断电行为 |
| 有界重试 | [sethvargo/go-retry](https://github.com/sethvargo/go-retry)，仓库已有 v0.4.0 | 直接复用已有指数退避、上限、full jitter 和 context 取消 | 重试策略不能把磁盘满或永久鉴权错误变成无限静默循环 |
| 原子状态文件 | 现有 `google/renameio/v2` | 继续用于小型状态/ACK 检查点，不自行写临时文件替换逻辑 | 检查点与队列的提交顺序需故障验证；不能覆盖尚未确认报告 |
| ClickHouse 客户端 | [clickhouse-go/v2](https://github.com/ClickHouse/clickhouse-go)，Apache-2.0 | 中央服务器直接复用连接池与批量写入 | 不自动解决累计快照归并或业务去重；本次只查文档，未获取或试用其代码 |

建议先比较 tidwall/wal 与 go-diskqueue，不同时引入两套存储。偏向 WAL 加一个小型发送队列适配器，因为能够把“读取”和“确认后清理”明确分开；这是候选建议，尚未通过完整选型门槛。

tidwall/wal 当前获取的实现默认 `NoSync=false`，写入会同步文件；不能为性能直接关掉后仍声称掉电不丢数据。其部分路径使用段缓存，需要测量节点峰值内存。还要验证尾部截断、损坏恢复、目录同步及单进程文件所有权，不能只因 README 写 durability 就认定生产可用。

## 最值得读的现成实现

### Vector：确认后回收，而非读出后回收

[disk buffer v2 源码](https://github.com/vectordotdev/vector/blob/403f671bfa330b41890f6ef6b7c42fb33fdace14/lib/vector-buffers/src/variants/disk_v2/mod.rs)描述记录校验、分段、ledger 和已确认文件回收。[端到端确认文档](https://vector.dev/docs/architecture/end-to-end-acknowledgements/)说明确认必须贯穿 source 和 sink；仅换成 socket/WebSocket 并不会获得持久保证。

参考其恢复不变量、缺口统计和 ACK 生命周期。Rust 实现且依赖 Vector 事件体系，不直接移植到 Go；Vector 为 MPL-2.0，复制文件时保留相应义务。中央 ClickHouse 管道可另评估直接运行 Vector，避免另写通用批量 sink。

### OpenTelemetry Collector：在途报告也属于未确认数据

[persistent_queue.go](https://github.com/open-telemetry/opentelemetry-collector/blob/adcbe6852f7f1aecddfdf5f31c57c00ba3e5ee85/exporter/exporterhelper/internal/queue/persistent_queue.go)与同目录测试值得重点参考：区分队列和正在发送项，处理重启后的恢复。[exporterhelper](https://github.com/open-telemetry/opentelemetry-collector/blob/adcbe6852f7f1aecddfdf5f31c57c00ba3e5ee85/exporter/exporterhelper/README.md)提供 batching、retry 和 overflow 行为。

其实现处于 `internal` 包，不能作为外部 Go 包直接 import；完整 Collector 会引入组件生命周期与 OTLP 数据模型。file_storage 当前为 beta，内部使用 bbolt，可配置 fsync。按仓库“节点不放数据库”的约束，这部分作为设计/测试参考，或用于中央服务，不能直接塞入薄 agent。

### Filebeat：明确的 ACK 游标和分段回收

[acks.go](https://github.com/elastic/beats/blob/e30e9beb7d3110cf4066a0300846189078c3b5f8/libbeat/publisher/queue/diskqueue/acks.go)、同目录 `consumer.go`、`checksum.go`、`state_file.go`：参考未确认位置、校验与恢复边界。获取的这些文件头为 Apache-2.0；项目根许可证为混合说明，不能笼统认定整个 Beats 可自由复制。

这套队列与 libbeat 框架耦合，直接依赖会扩大 agent。优先借鉴不变量和测试场景，不抽取几段代码就宣称获得完整 Filebeat 的保障。[队列配置文档](https://www.elastic.co/docs/reference/beats/filebeat/configuring-internal-queue)可用于核对背压、分段和预读参数的取舍。

### Fluent Bit：成熟的 chunk ACK 协议

[Forward 输出实现](https://github.com/fluent/fluent-bit/blob/a415d1b089d13d56366443a480e7433e9fdd27e6/plugins/out_forward/forward.c)以及[协议配置](https://docs.fluentbit.io/manual/data-pipeline/outputs/forward)支持 chunk 标识与 `Require_ack_response`。借鉴批次标识、确认匹配与失败重试；ACK 保证到哪一层仍需接收端持久化承诺，不能推断为 ClickHouse 已落盘。

这是 C 采集器实现，不适合直接嵌入 Go。若未来把普通 system logs 单独交给成熟采集器，可评估运行 Fluent Bit；连接归属和计费仍保留 BoxFleet。额外节点进程的内存需实测。

## 已做的小型恢复试验

本地参考代码与许可证保存于 `refs/telemetry-prior-art/`，该目录沿用仓库规则被 Git 忽略。固定提交、文件 URL 和 SHA-256 记录在 [manifest](references/telemetry-prior-art-manifest.json)，用于重新获取和核验；未将第三方代码复制进业务包。

go-diskqueue 试验：子进程写入，执行 Peek 或 Read，等待配置的同步周期后直接退出，不调用 Close，再由父进程重新打开。

| 场景 | 结果 |
| --- | --- |
| Peek 后尚未业务 ACK，进程退出 | 报告仍可恢复并重放 |
| Read 后尚未业务 ACK，进程退出 | 报告已消费，不再重放，说明这种用法有丢失窗口 |
| 恢复后模拟 ACK，再消费 | 队列归零 |

上述两项行为测试通过；它们只验证进程退出恢复和消费语义，不是物理断电、fsync 失败、磁盘满、损坏或长期压力验证。不能据此宣布库已经适合生产。当前试验为单消费者、单在途报告。

可运行 `python3 scripts/fetch-telemetry-prior-art.py` 按 manifest 获取参考文件，再运行 `go -C refs/telemetry-prior-art/poc test -v ./...`。脚本只获取参考资料，不修改项目依赖或部署服务。

## BoxFleet 自己保留的薄适配层

直接复用传输与存储库后，只实现业务必须的部分：

- 共享报告封装、稳定 ID、序号、schema version、批次大小上限。
- 从 token 获取 node_id；HTTP/WSS 共用验证、事务与幂等逻辑。
- 已收报告 immutable；ACK 丢失后重发原内容，不能把新窗口合入同一报告 ID。
- 明确 ACK 的持久边界；确认后推进检查点，再回收。崩溃允许重发，不允许提前删掉未确认数据。
- 累计快照 MAX 合并、旧增量桶及 coverage 去重，沿用现有来源切换规则。
- 容量上限、背压、数据缺口、队列年龄、磁盘错误及最后上报时间的可观测性。

WSS 不是前提。先用现有 HTTP 验证磁盘队列，再加 WSS；复用现有 auth、共享 model、服务端报告唯一键、collector 和测试夹具。不重写配置拉取、sing-box 本机 gRPC 客户端及计费链路。

如中央管道需要独立多消费者与重放，可评估 NATS JetStream 或 Kafka，使用其客户端和确认协议；不能只因消息代理提供 ACK 就删除节点队列。第一阶段没有证据需要额外 broker。

## 接入前必须通过的门槛

固定库版本并审计维护状态、依赖、许可证；测试 ACK 前后各崩溃点、服务器提交后丢响应、重复和乱序、损坏尾记录、磁盘满与同步失败、长时间停机、重启及满队列恢复。测量内存、磁盘上限与每秒写入，并确认错误不会阻塞 sing-box 接收路径或代理业务。

不把“使用成熟库”等价为“整个软件已经完善”。库解决通用机制；最终完整性取决于适配顺序、配置及上游采集能力。
