# sys-mcp 当前代码实现复审

## 范围说明

本次是基于当前仓库代码的二次 review，重点复核上次指出的问题是否真正闭环，并继续找“设计目标”和“当前实现”之间仍然存在的出入。

已确认本轮已修复的点：

- 单机 MCP 工具主路径已经接入 `RouterBridge`
- `list_agents` 已过滤 `proxy` 节点
- 工具调用日志已接入单机工具主路径
- `CancelRequest` 已能从 center 传到 proxy / agent
- `/internal/forward` 已增加共享密钥校验

下面只保留这轮复审后仍成立的 findings。

## Findings

[P0] PostgreSQL agent 注册表仍未接到真实注册/心跳/下线路径，HA 仍然跑不通 - `cmd/sys-mcp-center/main.go:78`, `internal/sys-mcp-center/store/store.go:74`
原因:
- `RouterBridge` 现在已经会查 PostgreSQL 做跨实例转发，但 `agent_instances` 这张表仍然没有在 agent/proxy 注册、心跳、下线时被更新。
- 当前代码里只有 `tool_call_logs` 被业务主路径使用，`UpsertAgent`、`UpdateAgentHeartbeat`、`SetAgentOffline`、`DeleteAgent` 仍然没有接入 `tunnel_svc` 或 offline checker。
- 这意味着跨实例查路由时，数据库里大概率没有对应 agent 记录，`ForwardIfNeeded()` 仍会直接返回 “not found in database”。
证据:
- `cmd/sys-mcp-center/main.go:78-101`
- `internal/sys-mcp-center/ha/router_bridge.go:24-47`
- `internal/sys-mcp-center/store/store.go:74-116`
- `rg` 结果显示上述 4 个 store 方法当前仍只有测试在调用
建议:
- 先把 `tunnel_svc.Connect()` 中的注册、心跳、断连注销，和 offline checker 的离线标记全部写入 `store.Store`。
- 在这条链路没打通前，`center HA 跨实例路由` 仍不能算真正完成。

[P1] 多机工具仍绕过了你刚补上的 HA 和 node_type 约束 - `internal/sys-mcp-center/mcp/server.go:255`
原因:
- 单机工具已经接入 `RemoteForwarder`，但 `registerMultiTool()` 还是旧逻辑，没有使用 `fwd`，也没有做日志接入。
- 当 `target_hosts` 为空时，它会把所有在线节点都塞进 `records`，没有过滤 `NodeType != "agent"`，于是 proxy 节点会重新混入批量调用面。
- 当显式传入 `target_hosts` 时，也只做 `reg.Lookup()`，不会走跨实例转发，因此多 center 场景下批量调用仍然只覆盖本机内存 registry。
证据:
- `internal/sys-mcp-center/mcp/server.go:59-61`
- `internal/sys-mcp-center/mcp/server.go:255-305`
- 对比单机工具路径：`internal/sys-mcp-center/mcp/server.go:162-176`
建议:
- 把单机工具的过滤/转发策略统一下沉到多机工具，至少保证：
  1. 只选 `node_type=agent`
  2. 多 center 场景下能覆盖远端 agent
  3. 批量路径与单机路径共用一套调用日志和错误语义

[P1] 取消协议虽然打通了，但大多数文件工具并不真正响应 `ctx`，超时后仍可能继续跑 - `internal/sys-mcp-agent/agent.go:118`, `internal/sys-mcp-agent/fileops/readfile.go:39`
原因:
- agent 现在已经保存 `request_id -> cancelFn`，收到 `CancelRequest` 也会执行 `cancel()`，这个方向是对的。
- 但 `read_file`、`list_directory`、`stat_file`、`check_path_exists`、`search_file_content` 的主体逻辑基本都没有检查 `ctx.Done()`；`search_file_content` 甚至会先把整文件读入 `allLines` 再做匹配。
- 结果是：center 超时后虽然协议上发出了取消，但文件类 handler 依旧可能跑到自然结束，取消只停在 context 层，没有落实到执行层。
证据:
- `internal/sys-mcp-agent/agent.go:118-164`
- `internal/sys-mcp-agent/fileops/readfile.go:39-121`
- `internal/sys-mcp-agent/fileops/readfile.go:125-156`
- `internal/sys-mcp-agent/fileops/search.go:47-149`
- `internal/sys-mcp-agent/fileops/listdir.go:31-68`
- `internal/sys-mcp-agent/fileops/stat.go:29-106`
建议:
- 对所有可能遍历文件/目录的循环加入 `select { case <-ctx.Done(): ... }`。
- `search_file_content` 不要先把整文件读入内存；至少改成流式扫描，并在扫描/匹配循环里尊重 `ctx`。

[P1] 文件访问控制仍然只做字符串前缀判断，symlink 逃逸问题没有解决 - `internal/sys-mcp-agent/fileops/guard.go:17`
原因:
- `PathGuard.Check()` 依旧只是 `filepath.Clean()` 后做前缀匹配，没有 `EvalSymlinks()`，也没有在打开文件后校验真实落点。
- 允许目录内部如果存在指向敏感路径的 symlink，`read_file` 等操作依然可能被绕过到真实目标。
- 这和项目文档里的“最小权限原则，默认只读，安全优先”仍有明显距离。
证据:
- `internal/sys-mcp-agent/fileops/guard.go:17-63`
- `internal/sys-mcp-agent/fileops/readfile.go:44-60`
- `internal/sys-mcp-agent/fileops/search.go:53-77`
建议:
- 在授权判断时引入真实路径解析，并明确 symlink 策略。
- 如果暂时不想支持 symlink，最简单的生产友好做法是直接拒绝访问最终落点为 symlink 的路径。

[P1] 内部转发仍固定走明文 HTTP，和 center 已有 TLS 配置不一致 - `internal/sys-mcp-center/ha/forwarder.go:48`
原因:
- `/internal/forward` 现在有共享密钥，这比上次好很多；但发起方 URL 仍然被硬编码成 `http://`。
- 同一个 center 进程明明已经支持 `ListenAndServeTLS()`，但实例间转发不会跟随 TLS 配置，等于把 HA 通道单独降级成明文。
- 在设计目标里，中心服务是统一控制面，这条内部通道继续明文会让实际部署边界和文档不一致。
证据:
- `cmd/sys-mcp-center/main.go:151-171`
- `internal/sys-mcp-center/ha/forwarder.go:48-58`
建议:
- 至少让内部转发根据配置决定 `http` / `https`。
- 更彻底的做法还是把内部转发切回独立的内部 gRPC 通道，而不是复用外部 HTTP 监听。

## Open Questions

- 你现在是否明确只想先支持“单 center + 远期 HA 预埋”，还是已经希望这套代码能直接支撑多 center 实际部署？这会影响上面第一个 P0 的优先级。
- 多机工具是否有意只做“本实例 fan-out”，还是原本就想和单机工具一样具备跨实例能力？当前代码和设计文档在这点上还没有完全对齐。

## Summary

这轮和上轮相比，确实已经修掉了几条关键缺口，方向是对的。但剩下的核心问题也更明确了：HA 的“调用路径”接上了，`agent_instances` 这个“状态源”还没接上；取消协议接上了，具体文件 handler 还不真正响应取消；单机工具修正了 node_type/HA，多机工具还停在旧逻辑。下一步最值得优先补的是：数据库注册表落地、多机工具语义对齐、文件工具真正尊重 `ctx`。
