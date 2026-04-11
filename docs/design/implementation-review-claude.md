# sys-mcp 实现 vs 设计 Review（Claude）

> 初次审查：2026-04-11（全仓库实现对照设计文档）
> 第二轮审查：2026-04-11（对照本轮 diff：15 个文件，+911/-397 行）
> 所有结论已回到源码验证，构建与测试均通过

---

## 第二轮 Review（本轮 diff）

### 已修复项（来自初轮 review）

- P0: RouterBridge 接入主路径 — `mcp/server.go` 在 `rec == nil` 时调用 `fwd.ForwardIfNeeded()`，HA 跨实例路由生效。
- P0: proxy 心跳转发 — `tunnel_svc.go` 对已知 agent 的 RegisterRequest 改走 `UpdateHeartbeat()`，不再重置 `RegisteredAt`。
- P0: CancelRequest 全链路 — `router.go` 超时时发 CancelRequest；`agent.go` 通过 `cancelFns sync.Map` 接收取消；`downstream.go` 新增 `DeliverCancelRequest` + `pendingRequests` 映射路由取消。
- P1: `list_agents` 过滤 proxy — 加了 `node_type != "agent"` 过滤，`Online` 计数也同步修正。
- P1: HA HTTP 转发鉴权 — `X-Internal-Auth` header + `ha.internal_secret` 配置；`internalHTTPClient` 替换 `http.DefaultClient`，有连接池和 idle 超时。
- P1: `tool_call_logs` 写入接入 — `CallLogger` 接口注入，`store.Store` 实现此接口并在工具调用前后写入。

---

## Findings（本轮新引入）

### [P1] CancelRequest 可能静默失效 — `agent.go:137`

`cancelFns.Store(req.RequestId, cancel)` 在 goroutine 内部执行：

```go
go func() {
    ctx, cancel := context.WithTimeout(...)
    a.cancelFns.Store(req.RequestId, cancel)  // goroutine 内
    ...
}()
```

如果 center 超时很短（如 1s），在 goroutine 尚未被调度执行（`Store` 还没跑到）时 CancelRequest 已到达，`dispatch` 中的 `LoadAndDelete` 找不到条目，cancel 被静默忽略，goroutine 继续运行直到 `ToolTimeoutSec`。

修复：将 `cancelFns.Store` 移到 `go func()` 之前，在分发 goroutine 的调用方注册：

```go
ctx, cancel := context.WithTimeout(context.Background(), timeout)
a.cancelFns.Store(req.RequestId, cancel)
go func() {
    defer func() {
        cancel()
        a.cancelFns.Delete(req.RequestId)
    }()
    ...
}()
```

---

### [P1] `pendingRequests` 上游重连后不清理 — `downstream.go:ReregisterAll`

`ReregisterAll` 在上游重连后调用，但 `pendingRequests` 中遗留的 in-flight requestID 不会被清理。旧请求的 ToolResponse/ErrorResponse 永远不会到来（上游 stream 已断），这些条目永久驻留在 `pendingRequests`。在 proxy 长时间运行且上游频繁重连（网络抖动）的场景下会持续累积，造成缓慢内存泄漏。

修复：在 `ReregisterAll` 开头（或 `OnRegisterAck` 重连回调中）清理 `pendingRequests`：

```go
func (s *DownstreamService) ReregisterAll(ctx context.Context) {
    s.pendingRequests.Range(func(k, _ any) bool {
        s.pendingRequests.Delete(k)
        return true
    })
    // ... 原有逻辑
}
```

---

### [P2] `internalHTTPClient.Timeout` 无效 — `ha/forwarder.go:15`

```go
var internalHTTPClient = &http.Client{
    Timeout: 35 * time.Second,
    ...
}
```

调用时使用 `context.WithTimeout(ctx, 30*time.Second)`，context 会先在 30s 触发，client-level 的 35s timeout 永远不会生效（context 更短）。这个配置项产生误导，让人以为有两层保护，实际只有 context 在控制。建议去掉 `Timeout` 字段，由调用方 context 统一控制，或把两处对齐为同一个值。

---

### [P2] `TestListAgentsResult_ProxyFiltered` 测试的是副本逻辑，不是实际 handler — `mcp/server_test.go:86`

该测试在测试文件内重新实现了过滤逻辑（`if r.NodeType != "agent" { continue }`），断言自己的实现结果。如果 `mcp/server.go` 的过滤条件被修改（如改成 `node_type == "proxy" { continue }`，逻辑等价但依赖字段不同），测试不会失败。

建议改为通过 `mcp.NewMCPHandler` 构造 handler，调用 `list_agents` 工具，验证返回的 JSON 中不含 `node_type=proxy` 的条目。这样才能覆盖到实际代码路径。

---

## 遗留项（初轮 review 未修复，多为设计文档同步问题）

| 项目 | 严重度 | 说明 |
|------|--------|------|
| schema 迁移未使用 goose | P1 | 设计文档指定 goose，实现用内嵌 SQL 直接 exec，无版本管理 |
| BatchRegisterRequest/Ack 未实现 | P1 | proto 无此消息，proxy 逐条发注册；设计文档应同步 |
| stream_generation 防幽灵路由 | P1 | proto/schema 均无此字段；split-brain 场景下旧 stream 响应可污染结果 |
| ProxyPath 追加方向 | P2 | `append([]string{proxyHostname}, req.ProxyPath...)` 把自己插最前，与设计"追加"措辞不符，语义未明确定义 |
| multi-tool 只覆盖 2/7 | P2 | 设计说所有工具应支持 `target_hosts` 数组，当前只有 `_multi` 变体 2 个 |

---

## Summary

本轮修复了初轮 review 全部 P0 项和主要 P1 项，质量提升明显。

新引入的两个 P1 问题值得优先处理：cancel 注册时序（CancelRequest 可能在 goroutine 注册 cancel 函数之前到达而被丢弃）和 proxy 重连后 `pendingRequests` 泄漏。两者修复都是小改动，风险低。

遗留的 stream_generation 机制涉及 proto 变更，是最复杂的未完成项，建议单独排期。
