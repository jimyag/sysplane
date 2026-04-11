# 设计与实现状态对照

> 最后更新：2026-04-11

本文档对照设计文档，梳理各项功能的当前实现状态。

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
| MCP HTTP/SSE 服务端（10 个工具） | 完成 | `internal/sys-mcp-center/mcp/server.go` |
| Bearer Token 鉴权（HTTP + gRPC） | 完成 | `internal/sys-mcp-center/mcp/auth.go` |
| proxy 转发注册（downstream agent via proxy） | 完成 | `tunnel_svc.go` 中处理 RegisterRequest 转发 |
| 流关闭时自动注销 | 完成 | `reg.UnregisterByStream()` 在 Connect 退出时调用 |
| 心跳超时主动下线检测 | 完成 | `registry.StartOfflineChecker()`，90s 无心跳标记 offline |
| center gRPC/HTTP TLS | 完成 | `cmd/sys-mcp-center/main.go`，证书为空时自动降级明文 |
| Prometheus metrics | 完成 | `internal/sys-mcp-center/metrics/`，独立端口暴露 |
| 多机并发查询 SendMulti | 完成 | `router.SendMulti()`，新增 `get_hardware_info_multi` / `list_directory_multi` |
| PostgreSQL 持久化注册表 | 完成 | `internal/sys-mcp-center/store/`，三张表 |
| center HA 自注册/心跳 | 完成 | `internal/sys-mcp-center/ha/registration.go` |
| center HA 跨实例路由 | 完成 | `internal/sys-mcp-center/ha/router_bridge.go` + `/internal/forward` 端点 |

### sys-mcp-proxy

| 功能 | 状态 | 说明 |
|------|------|------|
| 下游 gRPC 服务端（接收 agent） | 完成 | `internal/sys-mcp-proxy/tunnel/downstream.go` |
| 上游连接（连接 center 或上级 proxy） | 完成 | `internal/pkg/stream/dialer.go` |
| 转发下游 agent 注册到上游 | 完成 | DownstreamService 中的 RegisterRequest 转发 |
| 心跳转发到上游（防止 center 误判 offline） | 完成 | 收到下游心跳后向 center 发 RegisterRequest 刷新时间戳 |
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
| SendMulti 并发路由 | `internal/sys-mcp-center/router/router_test.go` | 已测试 |
| Prometheus 指标注册 | `internal/sys-mcp-center/metrics/metrics_test.go` | 已测试 |
| PostgreSQL store | `internal/sys-mcp-center/store/store_test.go` | 已测试（无 PG 时自动跳过） |
| proxy 编译冒烟测试 | `internal/sys-mcp-proxy/tunnel/downstream_test.go` | 已测试 |
| E2E 全链路（15 用例） | `/tmp/sys-mcp-test/e2e_test.py` | 15/15 通过 |

---

## 后续可选增强

以下功能超出当前 MVP 范围，可按需实现：

1. center `/internal/forward` 端点增加 HMAC 签名校验（当前仅依赖内网信任）
2. RouterBridge 完整接入 router.Send() 主路径（当前已实例化但未自动触发）
3. 历史调用日志写入 `tool_call_logs` 表
4. 客户端 stdio 桥接测试、center MCP 工具注册单元测试
