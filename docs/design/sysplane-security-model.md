# Sysplane 一期安全模型

本文档定义 Sysplane 一期安全模型。  
一期目标很明确：只做 token 鉴权与最基本的运行时限制，不引入 RBAC、用户组、节点级权限绑定、路径权限控制或审批流。

本文档服务于以下问题：

- center 如何区分不同调用方
- 哪些 token 可以访问哪些入口
- agent / proxy 如何向上游注册
- 高风险能力如何在没有 RBAC 的前提下收住边界
- 一期最小运行时限制如何生效

相关文档：

- [docs/design/sysplane-vision.md](/Users/jimyag/src/github/jimyag/sys-mcp/docs/design/sysplane-vision.md)
- [docs/design/sysplane-api-v1.md](/Users/jimyag/src/github/jimyag/sys-mcp/docs/design/sysplane-api-v1.md)

---

## 一、一期范围

一期只包含以下安全能力：

- Bearer token 鉴权
- token 分域
- 管理面与业务面分 token
- 最基本的运行时限制
- 审计记录与 request_id 关联

一期明确不做：

- RBAC
- OIDC / SSO
- 用户身份体系
- 节点级权限绑定
- 命令审批流
- 动态策略下发控制台

换句话说，一期判断逻辑是：

1. 这个 token 是否有效
2. 这个 token 属于哪一类
3. 这类 token 是否允许访问当前入口或动作
4. 当前调用是否满足最小运行时限制

---

## 二、设计原则

### 2.1 简单且可落地

一期避免引入复杂授权引擎，center 只做静态 token 校验和固定入口控制。

### 2.2 分域优先

不同流量面必须使用不同 token 集合，至少要避免以下混用：

- 业务 API token 与管理 API token 混用
- HTTP 调用 token 与 agent 注册 token 混用
- 下游 agent token 与上游 proxy 自身 token 混用

### 2.3 center 前置，agent 约束

安全检查分两层：

- center 负责识别 token、拒绝非法入口访问、阻止明显高风险调用
- agent 负责执行超时、并发等最小运行时约束

### 2.4 先收边界，再谈精细化

一期优先保证“不能乱进、不能乱调、不能失控执行”，而不是追求复杂的细粒度权限模型。

---

## 三、token 分域

一期建议至少区分四类 token。

### 3.1 `client token`

用途：

- CLI 调用业务 API
- MCP 适配层调用业务 API
- 自动化脚本调用业务 API

允许访问：

- `/v1/nodes`
- `/v1/invocations`
- `/v1/command-templates/{id}:invoke`
- 未来面向调用方的只读查询 API

默认不允许：

- 创建或修改命令模板
- 修改策略
- 管理 token
- 任何管理面 API

### 3.2 `admin token`

用途：

- Web/Admin 后端调用管理 API
- 运维管理脚本调用管理 API

允许访问：

- 全部 `client token` 能访问的业务 API
- `/v1/command-templates`
- `/v1/policies`
- `/v1/audit/events`
- 其他管理类 API

额外能力：

- 创建高风险命令模板
- 调用高风险内置动作

### 3.3 `agent token`

用途：

- agent 向 center 注册
- agent 向 proxy 注册

允许访问：

- 仅限隧道注册与心跳维持链路

默认不允许：

- 调用任何 HTTP 业务 API
- 访问管理 API

### 3.4 `proxy token`

用途：

- proxy 向 center 或上级 proxy 注册

为什么单独拆分：

- proxy 是网络转发节点，不应与普通 agent 共用完全同一类凭证
- token 泄露时需要能单独撤销 proxy，而不影响 agent
- center 需要知道连接上来的是 agent 还是 proxy

允许访问：

- 仅限隧道注册、心跳维持、转发相关上游链路

默认不允许：

- 调用任何 HTTP 业务 API
- 访问管理 API

---

## 四、center 配置模型

一期建议 center 配置显式拆分 token 集合，不再只保留 `client_tokens` 与 `agent_tokens` 两类。

建议结构：

```yaml
auth:
  client_tokens:
    - "client-token-1"
    - "client-token-2"
  admin_tokens:
    - "admin-token-1"
  agent_tokens:
    - "agent-token-1"
    - "agent-token-2"
  proxy_tokens:
    - "proxy-token-1"
```

字段语义：

- `client_tokens`
  - 业务调用 token 列表
- `admin_tokens`
  - 管理调用 token 列表
- `agent_tokens`
  - agent 注册 token 列表
- `proxy_tokens`
  - proxy 注册 token 列表

实现要求：

- 任意 token 不能为空字符串
- 不同域的 token 不应复用同一个值
- 启动时应检测重复 token 并拒绝启动或至少打印高优先级错误

---

## 五、请求与连接上的携带方式

### 5.1 HTTP API

HTTP API 使用标准 Bearer token：

```text
Authorization: Bearer <token>
```

center 的处理顺序：

1. 读取 `Authorization`
2. 校验格式是否为 `Bearer <token>`
3. 在 `client_tokens` 与 `admin_tokens` 中查找
4. 推导 `token_type=client|admin`
5. 根据目标 API 路径做准入判断

### 5.2 agent / proxy 上游连接

agent 与 proxy 向上游建连时，在首次注册消息中携带注册 token。

逻辑要求：

- `node_type=agent` 时，只能使用 `agent_tokens`
- `node_type=proxy` 时，只能使用 `proxy_tokens`
- token 不匹配时，上游立即拒绝注册

这样可以避免“拿 agent token 冒充 proxy”或“拿 proxy token 冒充 agent”的模糊状态。

### 5.3 proxy 对下游的校验

一期不建议 proxy 完全透传下游注册而不做任何预校验。

建议行为：

- proxy 维护自己的 `auth.agent_tokens` 与 `auth.proxy_tokens`
- 下游 agent / proxy 接入 proxy 时，proxy 先做本地 token 校验
- 校验通过后，再与上级维持自己的上游连接

原因：

- 可以减少无效连接把 proxy 打成放大器
- 可以更早拒绝错误配置
- 与 center 的 token 分域模型一致

---

## 六、入口准入规则

一期不做细粒度授权，只做固定的入口与动作控制。

### 6.1 HTTP 路径准入

建议准入矩阵：

```text
GET  /v1/nodes                          client/admin
GET  /v1/nodes/{id}                     client/admin
GET  /v1/nodes/{id}/capabilities        client/admin
POST /v1/invocations                    client/admin
GET  /v1/invocations/{id}               client/admin
GET  /v1/invocations/{id}/results       client/admin
POST /v1/invocations/{id}:cancel        client/admin

GET  /v1/command-templates              client/admin
GET  /v1/command-templates/{id}         client/admin
POST /v1/command-templates              admin
PATCH /v1/command-templates/{id}        admin
POST /v1/command-templates/{id}:invoke  client/admin

GET  /v1/audit/events                   admin
GET  /v1/audit/events/{id}              admin
```

说明：

- `client token` 可以使用平台能力，但不能改平台配置
- `admin token` 同时具备业务调用与管理操作能力
### 6.2 高风险动作准入

即使 HTTP 路径允许访问，也还需要按动作再收一次。

建议规则：

- `client token`
  - 允许只读内置动作
  - 允许调用普通命令模板
  - 不允许文件写入
  - 不允许调用 `risk_level=dangerous` 的模板
- `admin token`
  - 可调用高风险动作
  - 但仍必须满足运行时限制

这意味着：

- `FORBIDDEN` 表示 token 类型不允许
- `POLICY_DENIED` 表示 token 虽然允许，但运行时限制拒绝

---

## 七、最小运行时限制

一期不做路径级权限控制，也不做 agent 侧的执行开关。  
center 的 token 鉴权通过后，请求仍然要满足最基本的资源限制。

### 7.1 最小限制字段

建议最小字段：

```yaml
security:
  max_read_bytes: 1048576
  max_exec_timeout_sec: 30
  max_concurrency: 8
```

字段语义：

- `max_read_bytes`
  - 单次读取最大字节数
- `max_exec_timeout_sec`
  - 单次执行最大超时
- `max_concurrency`
  - 并发执行上限

### 7.2 读写与执行语义

一期约定：

- 不做 `blocked_paths`
- 不做 `allowed_paths`
- 不做 `allow_exec`
- 不做 `allow_write`

也就是说：

- 命令执行默认允许进入执行链路
- 文件访问与文件写入是否开放，由具体 API/模板能力和 token 类型决定
- agent 不再额外承担路径权限控制

### 7.3 超时与并发

建议规则：

- 实际执行超时取 `min(request_timeout, max_exec_timeout_sec)`
- 超出并发上限时，应快速失败，不要无限排队

---

## 八、鉴权流程

### 8.1 HTTP API

标准流程：

1. 解析 `Authorization` 头
2. 校验 token 是否存在于 `client_tokens` 或 `admin_tokens`
3. 标记 `token_type`
4. 校验该 `token_type` 是否允许访问当前路径
5. 若为执行请求，再校验是否允许该动作类型
6. 创建审计事件
7. 路由到 agent
8. agent 校验运行时限制
9. 返回结果

### 8.2 agent / proxy 注册

标准流程：

1. 下游发起上游连接
2. 首包包含 `node_type` 与注册 token
3. 上游根据 `node_type` 选择对应 token 集合
4. 校验通过则接受注册
5. 校验失败则立即关闭连接

### 8.3 鉴权失败语义

建议行为：

- token 缺失或值非法：`401 UNAUTHORIZED`
- token 类型不允许访问目标入口：`403 FORBIDDEN`
- token 可以访问入口，但动作被安全规则拦截：`403 FORBIDDEN`
- token 与入口均允许，但运行时限制拒绝：`403 POLICY_DENIED`

---

## 九、审计要求

一期即使没有 RBAC，也必须有完整审计。

建议每条执行类审计事件至少包含：

- `request_id`
- `invocation_id`
- `token_type`
- `subject_id`
- `source`
- `action`
- `risk_level`
- `target_node_ids`
- `decision`
- `decision_reason`
- `created_at`
- `finished_at`

说明：

- `subject_id` 一期可以是 token 对应的逻辑名称或配置名
- 如果当前实现拿不到稳定 `subject_id`，至少要记录 `token_type` 与 token 指纹摘要
- 禁止把原始 token 直接写入日志或审计存储

---

## 十、token 生成、存储与轮转

### 10.1 生成要求

建议：

- 使用足够长的随机字符串
- 长度至少 32 字节随机熵
- 避免可读词汇、固定前缀、环境名拼接后当作 token

### 10.2 存储要求

一期可接受以明文配置方式部署，但应满足：

- 配置文件权限最小化
- 不写入普通日志
- 不出现在命令行参数中
- 不提交到仓库

### 10.3 轮转策略

建议使用“双 token 窗口”轮转：

1. 在 token 列表中先加入新 token
2. 更新调用方或下游节点配置
3. 验证新 token 生效
4. 再删除旧 token

### 10.4 撤销语义

一期需要明确一个现实约束：

- 撤销 token 只保证后续新请求或新连接失效
- 是否立即踢掉现有长连接，取决于实现复杂度

建议一期先采用：

- HTTP 请求实时校验，撤销立即生效
- gRPC/隧道长连接对已建立连接不做即时踢出
- 若需要强制失效，通过人工重启连接或后续补“连接重校验”

---

## 十一、错误码建议

安全相关错误建议统一使用以下业务错误码：

- `UNAUTHORIZED`
  - 没有 token 或 token 不存在于允许集合
- `FORBIDDEN`
  - token 类型不允许访问当前入口或动作
- `POLICY_DENIED`
  - 运行时限制拒绝
- `RATE_LIMITED`
  - 命中并发或流量保护

不建议：

- 把所有失败都返回成 `UNAUTHORIZED`
- 把 token 类型不允许和运行时限制拒绝混成一个错误

---

## 十二、与当前实现的关系

当前仓库与一期目标态的主要差异：

1. 现有文档和配置主要只有 `client_tokens`、`agent_tokens`
2. `proxy` 是否做下游 token 预校验仍不一致
3. 业务面与管理面的 token 分域尚未完全落定
4. token 撤销对现有长连接的即时性没有统一语义

一期建议的实现收敛方向：

1. center 配置新增 `admin_tokens`、`proxy_tokens`
2. proxy 对下游注册做本地 token 预校验
3. HTTP 中间件在请求上下文中显式写入 `token_type`
4. 所有执行路径统一产出安全审计字段

---

## 十三、后续扩展

一期完成后，可按顺序扩展：

1. token 指纹与描述字段
2. token 过期时间
3. 连接级强制撤销
4. OIDC / SSO
5. RBAC
6. 审批流

---

## 十四、文档状态

| 项目 | 说明 |
| ---- | ---- |
| 状态 | 一期安全模型草案 |
| 当前范围 | token 分域、入口准入、最小运行时限制 |
| 不含内容 | RBAC、审批、OIDC |
