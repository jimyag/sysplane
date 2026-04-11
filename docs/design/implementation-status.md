# 设计与实现状态对照

本文档对照设计文档，梳理各项功能的当前实现状态，区分已完成、部分实现和尚未实现的内容。

---

## 已完成的核心功能

### gRPC 隧道协议

| 功能 | 状态 | 说明 |
|------|------|------|
| 双向流 TunnelService | 完成 | `api/proto/tunnel.proto` + `api/tunnel/` |
| RegisterRequest / RegisterAck | 完成 | agent、proxy 注册流程 |
| ToolRequest / ToolResponse / ErrorResponse | 完成 | 工具调用全链路 |
| Heartbeat / HeartbeatAck | 完成 | 心跳保活 |
| target_host 字段（proxy 路由） | 完成 | ToolRequest.target_host |

### sys-mcp-agent

| 功能 | 状态 | 说明 |
|------|------|------|
| 连接上游（center / proxy） | 完成 | `internal/sys-mcp-agent/agent.go` |
| 指数退避自动重连 | 完成 | `internal/pkg/stream/dialer.go` |
| center 重启后自动重注册 | 完成 | Dialer 在每次新连接时发送 RegisterRequest |
| get_hardware_info | 完成 | `internal/sys-mcp-agent/collector/hardware.go` |
| list_directory | 完成 | `internal/sys-mcp-agent/fileops/listdir.go` |
| read_file | 完成 | `internal/sys-mcp-agent/fileops/readfile.go` |
| stat_file | 完成 | `internal/sys-mcp-agent/fileops/stat.go` |
| check_path_exists | 完成 | `internal/sys-mcp-agent/fileops/stat.go` |
| search_file_content | 完成 | `internal/sys-mcp-agent/fileops/search.go` |
| proxy_local_api | 完成 | `internal/sys-mcp-agent/apiproxy/proxy.go` |
| 路径访问控制（blocked/allowed paths） | 完成 | `internal/sys-mcp-agent/fileops/guard.go` |
| 端口访问控制 | 完成 | `internal/sys-mcp-agent/apiproxy/proxy.go` |
| hostname 覆盖配置 | 完成 | `AgentConfig.Hostname` 字段 |
| mTLS 客户端连接 | 完成 | `agent.buildCredentials()` + `internal/pkg/tlsconf/` |

### sys-mcp-center

| 功能 | 状态 | 说明 |
|------|------|------|
| gRPC TunnelService 服务端 | 完成 | `internal/sys-mcp-center/tunnel_svc.go` |
| 内存 agent 注册表 | 完成 | `internal/sys-mcp-center/registry/registry.go` |
| 单机工具路由（单 target_host） | 完成 | `internal/sys-mcp-center/router/router.go` |
| MCP HTTP/SSE 服务端（8 个工具） | 完成 | `internal/sys-mcp-center/mcp/server.go` |
| Bearer Token 鉴权（HTTP + gRPC） | 完成 | `internal/sys-mcp-center/mcp/auth.go` |
| proxy 转发注册（downstream agent via proxy） | 完成 | `tunnel_svc.go` 中处理 RegisterRequest 转发 |
| 流关闭时自动注销 | 完成 | `reg.UnregisterByStream()` 在 Connect 退出时调用 |

### sys-mcp-proxy

| 功能 | 状态 | 说明 |
|------|------|------|
| 下游 gRPC 服务端（接收 agent） | 完成 | `internal/sys-mcp-proxy/tunnel/downstream.go` |
| 上游连接（连接 center 或上级 proxy） | 完成 | `internal/pkg/stream/dialer.go` |
| 转发下游 agent 注册到上游 | 完成 | DownstreamService 中的 RegisterRequest 转发 |
| ToolRequest 路由到正确下游 agent | 完成 | `DownstreamService.DeliverToolRequest()` |
| 上游断线重连后重新注册所有下游 agent | 完成 | `ReregisterAll()` 在 OnRegisterAck 回调中调用 |
| 多级级联 | 完成 | proxy 的 upstream 可指向另一个 proxy |

### sys-mcp-client

| 功能 | 状态 | 说明 |
|------|------|------|
| stdio MCP 服务端（供 AI 助手连接） | 完成 | `cmd/sys-mcp-client/main.go` |
| SSE 连接到 center | 完成 | `mcp.SSEClientTransport` |
| 工具转发（client 工具 → center 工具） | 完成 | 动态发现并注册所有 center 工具 |
| Bearer Token 注入 | 完成 | 自定义 `http.RoundTripper` |

---

## 部分实现

### TLS

| 功能 | 状态 | 说明 |
|------|------|------|
| agent 客户端 mTLS | 完成 | `buildCredentials()` |
| proxy 客户端 mTLS | 完成 | `DialerConfig.TLSCredentials` |
| center gRPC 服务端 TLS | 未接入 | `cmd/sys-mcp-center/main.go` 硬编码 `insecure.NewCredentials()`，配置字段存在但未读取 |
| center HTTP 服务端 TLS | 未接入 | `http.ListenAndServe` 未改为 `ListenAndServeTLS` |

**影响**：生产环境中，center 的 gRPC 和 HTTP 端口应启用 TLS。目前 center 只适合在内网或通过 NGINX/Caddy 等反向代理做 TLS 终止的场景使用。

---

## 尚未实现（设计中存在，代码中缺失）

### PostgreSQL 持久化（center 设计第 4、6 节）

设计文档描述了完整的 PostgreSQL 数据模型，包含 `agent_instances`、`tool_call_logs` 等表，用于跨 center 实例共享状态。当前实现为纯内存注册表，center 重启后所有 agent 记录丢失（agent 会自动重连，记录会恢复，但重启期间不可查）。

影响：
- center 无法做到真正的高可用（多实例）
- 没有历史调用记录

### center 高可用（HA）多实例路由（center 设计第 4.1、4.3、4.4 节）

设计文档描述了多 center 实例通过 PostgreSQL 或消息队列协调的方案（agent 可以连接到任意 center 实例，跨实例请求通过数据库或内部 gRPC 转发）。当前实现为单实例，无法水平扩展。

影响：center 是单点故障，重启会导致短暂不可用。

### 多机并发查询 SendMulti（center 设计第 8 节）

设计文档描述了 `target_hosts` 数组参数，允许一次工具调用同时查询多台机器并聚合结果。当前实现只支持 `target_host`（单机）。

影响：需要批量查询时，AI 必须多次调用同一工具。

### 可观测性 / Metrics（设计第 9 节）

设计文档规划了 Prometheus 指标（注册 agent 数、工具调用延迟、错误率等）和结构化日志字段。当前实现有基础的 `slog` 结构化日志，但无 metrics 端点。

### 心跳超时主动下线检测

当前实现：center 在收到 Heartbeat 时更新 `LastHeartbeat`，但没有后台 goroutine 定期扫描并将超时 agent 标记为 `StatusOffline`。agent 的 `StatusOffline` 目前只有在 gRPC 流关闭（`UnregisterByStream`）时才会触发，不依赖心跳超时。

---

## 测试覆盖状态

| 测试 | 文件 | 说明 |
|------|------|------|
| Dialer 重连（transport 层） | `internal/pkg/stream/dialer_test.go` | 已测试 |
| 注册表基本操作 | `internal/sys-mcp-center/registry/registry_test.go` | 已测试 |
| agent 断线重注册（center 侧） | `internal/sys-mcp-center/reregister_test.go` | 已测试（bufconn 集成测试） |
| 路径访问控制 | `internal/sys-mcp-agent/fileops/guard_test.go` | 已测试 |
| 文件操作 | `internal/sys-mcp-agent/fileops/fileops_test.go` | 已测试 |
| 硬件信息采集 | `internal/sys-mcp-agent/collector/hardware_test.go` | 已测试 |
| 本地 API 代理 | `internal/sys-mcp-agent/apiproxy/proxy_test.go` | 已测试 |
| TLS 配置加载 | `internal/pkg/tlsconf/tlsconf_test.go` | 已测试 |
| proxy ReregisterAll | 缺失 | 未测试 |
| client stdio 桥接 | 缺失 | 未测试 |
| center MCP 工具注册 | 缺失 | 未测试 |

---

## 后续计划

优先级从高到低：

1. center gRPC/HTTP TLS 接入（低风险，只需读取已有配置字段）
2. 心跳超时主动下线检测（加一个后台 goroutine，30 行左右）
3. 多机并发查询 `SendMulti`（需改 proto + router + MCP server）
4. Prometheus metrics 端点
5. PostgreSQL 持久化 + center HA（工程量较大，需单独立项）
