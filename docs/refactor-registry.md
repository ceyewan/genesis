# Genesis Registry 组件修复与优化开发指导 (Issue #15)

这份文档旨在指导开发者修复 Issue #15 中报告的 `registry` 组件严重缺陷。这些问题涉及服务注册的可靠性、Watch 机制的容错能力以及 gRPC 解析器的性能问题。

## 1. 核心问题摘要

根据 Issue #15 的审计，当前 `registry` 组件存在以下核心问题：

1.  **租约未续约 (Critical)**: `Register` 仅申请了一次 TTL，未启动 `KeepAlive`，导致服务在 TTL 后被移除。
2.  **Watch 脆弱 (Major)**: 网络波动导致 Watch channel 关闭时，goroutine 直接退出，无法自动重连。
3.  **读放大 (Major)**: `etcdResolver` 监听到任一事件都会全量拉取服务列表 (`GetService`)。
4.  **版本一致性 (Minor)**: 缓存更新未校验 Etcd Revision，高并发下可能覆盖新数据。

## 2. 修复方案与开发步骤

请按照以下步骤逐一进行代码重构。

### 步骤 1: 实现租约自动续约 (KeepAlive)

**目标文件**: `registry/registry.go` -> `Register` 方法

**现状**:
仅调用了 `r.client.Grant`，缺少保活逻辑。

**变更指导**:
1.  在 `Grant` 成功后，立即调用 `r.client.KeepAlive(ctx, lease.ID)` 获取 keepAlive 通道。
2.  启动一个后台 goroutine 消费该通道，处理续约响应。
3.  **关键点**: 如果 keepAlive 通道关闭（lease 失效或网络中断），应尝试重新注册或记录错误并触发告警/重试逻辑（视具体需求，目前至少应记录 Error）。
4.  该 goroutine 应在 `Deregister` 或 `Close` 时通过 Context 或 stop channel 优雅退出。

### 步骤 2: 增强 Watch 机制鲁棒性

**目标文件**: `registry/registry.go` -> `Watch` 方法

**现状**:
`for { select { case ... <-watchCh: ... } }` 循环中，若 `watchCh` 关闭或返回错误，当前逻辑可能退出或行为未定义。

**变更指导**:
1.  将 Watch 逻辑封装在一个外层循环中（`for { ... }`），实现断线重连。
2.  **重连策略**:
    *   如果 `watchCh` 关闭或发生非正常错误，休眠一段时间（如 1s 指数退避）后重新发起 `r.client.Watch`。
    *   **增量监听**: 记录最后一次处理的 Etcd Revision。重连时使用 `clientv3.WithRev(lastRev + 1)` 避免事件丢失或重复（可选，视一致性要求而定，若不加 Rev 可能会丢事件）。
3.  确保 `ctx` 取消时能正确退出外层循环。

### 步骤 3: 优化 Resolver 为增量更新 (解决读放大)

**目标文件**: `registry/resolver.go`

**现状**:
`start()` 方法中收到 `eventCh` 事件后，忽略事件内容，直接调用 `updateAddresses()` 全量拉取。

**变更指导**:
1.  **维护本地状态**: 在 `etcdResolver` 结构体中增加一个 map 存储当前地址列表：
    ```go
    localCache map[string]resolver.Address // key: instanceID
    ```
2.  **初始化**: 启动时先做一次全量 `GetService`，初始化 `localCache`。
3.  **增量处理**:
    *   在 `Watch` 循环中处理 `ServiceEvent`。
    *   **PUT 事件**: 解析 event 中的 `ServiceInstance`，更新 `localCache` 中对应的 entry。
    *   **DELETE 事件**: 根据 event 中的 ID，从 `localCache` 删除。
4.  **状态推送**: 每次更新 `localCache` 后，调用 `r.cc.UpdateState` 推送最新列表。
5.  **移除**: 删除原有的 `updateAddresses()` 全量拉取逻辑（或仅保留作为兜底/初始化）。

### 步骤 4: 引入 Revision 版本控制

**目标文件**: `registry/registry.go`

**现状**:
`updateCache` 直接覆盖 map。

**变更指导**:
1.  修改 `cache` 结构，使其能存储 revision 信息：
    ```go
    type cachedService struct {
        instances []*ServiceInstance
        revision  int64
    }
    // r.cache map[string]*cachedService
    ```
    或者在 `updateCache` 时判断 revision。
2.  **Watch 变更**: `clientv3.Event` 包含 `ModRevision`。
3.  在 `updateCache` 中，比较事件的 revision 和缓存中已有的 revision。只有当 `event.ModRevision > currentCacheRevision` 时才更新缓存。

## 3. 技术注意事项

*   **锁的粒度**: 在修改 `registry.go` 时，注意 `r.mu` 的使用，避免在调用外部回调（如果有）或耗时操作时持有锁。
*   **Context 管理**: 确保所有新启动的 goroutine 都能随 `Registry.Close()` 或 `Register` 的 Context 取消而退出，避免 goroutine 泄漏。
*   **Etcd 连接**: Watch 重连时如果底层 Etcd 连接断开，client v3 会自动重连，但 Watch channel 可能会关闭，需要应用层处理重建 Watcher。

## 4. 验收标准

1.  **长效注册**: 启动服务并注册，调整 TTL 为 5s，观察 1 分钟后 Key 是否依然存在（验证 KeepAlive）。
2.  **故障恢复**: 在 Watch 运行期间重启 Etcd 或模拟网络断开，恢复后服务列表应能自动同步（验证 Watch 重连）。
3.  **性能验证**: 启动 100 个服务实例，更新其中 1 个。Resolver 日志中不应出现全量获取的日志，且 gRPC 客户端能感知地址变更（验证增量更新）。
