# 组件可靠性调整清单

本清单记录代码审阅发现的问题、最小调整范围与验收标准。所有调整在
`fix/component-hardening` 分支进行，每个问题独立提交；完成后将状态改为已完成。
这不是发布声明，不新增指定下游、证据哈希或自动发布要求。

## 第一批：可靠性修复

| 编号 | 问题与范围 | 验收标准 | 状态 |
| :--- | :--- | :--- | :--- |
| H01 | `idgen/sequence.go` 的 Lua 浮点数导致 `2^53` 附近返回失真与上限失效；最小修复整数运算、批量乘法及 int64 溢出 | 大整数返回精确；耗尽不改变值及 TTL；批量溢出被拒绝 | 已完成 |
| H02 | `idgen` 忽略 Counter 创建错误，随后可能 panic；核对其他组件的指标失败策略 | 失败 Meter 降级到 discard 并记录日志；ID 生成不 panic | 已完成 |
| H03 | Redis Stream 广播 `FromLatest` 空读后重复使用 `$`，跳过两次读取间的消息 | 初始位置只解析一次；空读及恢复不重置游标；真实 Redis 回归测试 | 已完成 |

## 第二批：契约与接入体验

| 编号 | 问题与范围 | 验收标准 | 状态 |
| :--- | :--- | :--- | :--- |
| H04 | Multi 缓存固定 TTL 与跨实例失效可能产生陈旧读取；并发回填可能覆盖同实例的新状态 | 保留现有 TTL 策略，明确陈旧窗口与边界；防止同实例过期回填覆盖 Set/Delete；回归测试 | 已完成 |
| H05 | 活跃安全、兼容性文档仍引用历史 RC2/指定下游门禁；README 指向历史博客 | 现行文档与 release.md/CI 一致；历史链接保留并标注；不声称已做漏洞扫描 | 已完成 |
| H06 | MQ/DLock 生产示例导入 internal/testkit，外部不能直接复制 | 生产示例不导入测试工具；临时外部 module 编译验证 | 已完成 |
| H07 | 缺少跨组件选型、最小生产装配、故障排查指南 | 提供可执行的公开 API 示例和清晰的资源/安全/一致性边界 | 已完成 |

## 第三批：真实场景与边界验收

| 编号 | 问题与范围 | 验收标准 | 状态 |
| :--- | :--- | :--- | :--- |
| H08 | cached-query 只验证串行命中；Local 加分布式锁不能自然实现跨实例只回源一次 | 单进程请求合并与共享 Redis 缓存跨实例重建均有并发断言；错误传播、清理完整 | 已完成 |
| H09 | async-task 使用 Memory 去重，第一次成功即退出；没有验证重启后的去重 | Redis 持久去重；等待重复消息处理；重新创建消费者后的去重与临时失败重投 | 已完成 |
| H10 | resilient-service 只有人工 curl，没有验证治理状态 | 并发幂等、429/恢复、系统故障熔断/半开、业务错误不熔断自动测试；优雅退出 | 已完成 |
| H11 | grpc-governance 只有单实例停止，没有动态发现与恢复验收 | 多实例加入/退出/重新加入；真实 Etcd Watch、gRPC 调用恢复与生命周期测试 | 已完成 |
| H12 | idem.Consume 负 TTL 静默默认，Execute/Consume 的 nil 回调可能 panic；亚毫秒序列 TTL 截断 | 无效输入在副作用前返回可分类错误；零 TTL 默认不变；边界测试 | 已完成 |
| H13 | 缺少输入 Fuzz 与缓存/幂等/MQ 性能基线 | Fuzz 覆盖高风险解析；benchmark 可复跑并记录分配；不根据一次结果提前优化 | 已完成 |
| H14 | 示例编译/可选后端 smoke 不等于场景成功；缺少明确的无 Docker 快速入口 | 独立快速、真实场景、外部示例检查入口；CI 执行场景断言；全量普通/race 门禁不削弱 | 已完成 |

## 第四批：仅 Redis 功能增强

| 编号 | 问题与范围 | 验收标准 | 状态 |
| :--- | :--- | :--- | :--- |
| H15 | Redis Connector 无 ACL Username 与 TLS 注入入口 | ACL 认证、可信 CA、主机名校验、错误证书/凭证、TLS 配置复制均测试；默认不变；API 清单更新 | 已完成 |

## 全量验收中新发现的问题

| 编号 | 问题与范围 | 验收标准 | 状态 |
| :--- | :--- | :--- | :--- |
| H16 | NATS 容器只等待 ready 日志，宿主机映射端口尚不可用时就进行 Connector 单次连接 | fixture 等待端口及协议可用；不更改生产 Connect 的失败语义；普通/race 重复验收 | 已完成 |

## 不在本轮范围

- Auth 密钥轮换、其他 Connector 的 TLS、公开缓存加载框架、Outbox 组件。
- 删除现有组件或已发布弃用符号，扩大现有接口，改变 NewMulti 参数类型。
- 自动一致性广播、严格同步 Multi TTL、跨进程强一致缓存承诺。
- 发布新版本、推送分支或修改 GitHub Release。

## 验证要求

每个 Go 改动运行对应测试和 `make modernize-check`，每次提交只包含该问题及本清单的
状态更新。最终运行完整普通/race 测试、lint、API 基线/兼容性、文档链接、示例编译、
外部复制和场景验收。容器测试由 `internal/testkit` 自动管理，不执行 `make up`。
性能基线记录命令与运行环境，不设置未经验证的绝对性能门槛。

## 最终本地验证

H01–H16 已实现并完成对应验收。全量检查发现的 NATS fixture 就绪竞态由 H16 修复，
之后以下检查全部通过：

- `go test -count=1 ./...`
- `go test -race -p 1 -count=1 ./...`
- `go build ./...`、`go mod verify`、`go mod tidy` 无漂移。
- `make lint exported-comments modernize-check`
- `make api-inventory-check api-baseline-check api-compat-check`
- `make markdown-check buf-lint godoc-artifacts`、`actionlint`
- `make test-fast examples-check examples-external-check examples-scenarios`
- `make example-all`（仅可选后端 smoke，不替代真实场景测试）。
- 三组探索性 Fuzz 各运行 3 秒、两个 worker，无失败；性能基线三轮见
  [性能记录](./performance.md)。

本地环境为 Go `1.26.1`、macOS arm64、Node.js `25.9.0`；这不是 CI 固定
Go `1.26.0` / `1.26.5` 或 Node.js `22.23.2` 的验证替代。没有发布新版本，
没有执行未授权的网络漏洞扫描，也没有运行本轮未改动的完整可观测性 Compose 栈。

## PR #72 审阅修正

| 编号 | 问题与修正 | 验收标准 | 状态 |
| :--- | :--- | :--- | :--- |
| H17 | 外部 module 的 tidy 与普通编译共享一分钟预算，race CI 冷构建被杀掉；改为独立阶段与各五分钟预算 | 阶段日志包含命令、耗时和 context 错误；隔离普通构建缓存的验证通过 | 已完成 |
| H18 | Multi 等待全实例读写锁时不响应取消；使用可取消的读写额度协调 | 五个 KV 操作在竞争和已取消 context 下及时返回；防止旧回填覆盖的测试保持通过 | 已完成 |
| H19 | TLS Clone 共享策略切片；补齐协议、密码套件、曲线和 ECH 配置的隔离 | 原配置修改及 Option 重用不会改变其他 Connector；nil 切片语义不变 | 已完成 |
