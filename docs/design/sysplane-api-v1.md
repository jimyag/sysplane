# Sysplane API v1 设计草案

本文档定义 Sysplane 目标态的首版 HTTP API 草案，作为 `docs/design/sysplane-vision.md` 的落地补充。  
目标是先收敛统一资源模型、调用语义、错误模型与同步/异步执行约定，供 center、CLI、Web/Admin、MCP 适配层共享。

---

## 一、设计目标

API v1 需要满足以下目标：

- 为 `CLI`、`Web/Admin`、`MCP` 提供统一后端契约
- 明确节点查询、内置动作、命令模板、统一执行记录四类核心对象
- 支持同步与异步两种调用形态
- 为 token 分域、审计、高风险操作控制保留清晰挂点
- 保持资源数量可控，不追求一次覆盖所有能力

本版优先覆盖：

- `/v1/nodes`
- `/v1/command-templates`
- `/v1/invocations`

不在本版细化范围：

- Web 会话与前端页面协议
- OIDC 登录细节
- 流式日志输出协议
- 审批流

---

## 二、通用约定

### 2.1 Base URL

```text
https://<center-host>/v1
```

### 2.2 Content Type

请求与响应统一使用：

```text
Content-Type: application/json
```

文件下载、流式输出等特殊场景在后续版本单独定义。

### 2.3 认证

业务面 API 使用 Bearer token：

```text
Authorization: Bearer <token>
```

v1 不规定 token 的签发方式，但要求 center 至少能识别以下属性：

- `subject_id`
- `token_type`

### 2.4 请求标识

每个请求都应支持以下标头：

```text
X-Request-Id: <client-request-id>
```

规则：

- 调用方可传入
- center 未收到时自动生成
- 所有审计事件、日志、下游执行记录都应关联该值

### 2.5 时间与 ID

统一约定：

- 时间使用 `RFC3339`
- ID 使用平台生成的稳定字符串 ID

示例：

```text
node_01J0ABCDEF
cmdtpl_01J0ABCDEF
inv_01J0ABCDEF
evt_01J0ABCDEF
```

### 2.6 分页

列表接口统一采用游标分页。

请求参数：

- `limit`
- `cursor`

响应字段：

```json
{
  "items": [],
  "next_cursor": "opaque_cursor_or_empty"
}
```

默认 `limit=50`，最大值建议 `200`。

### 2.7 同步与异步

v1 统一支持两种执行模式：

- 同步：请求阻塞直到结果返回或超时
- 异步：立即返回 `Invocation`，调用方稍后查询结果

建议通过请求体中的 `async` 字段控制，而不是通过不同路径拆分。

---

## 三、通用对象模型

### 3.1 Node

```json
{
  "id": "node_01J0A1",
  "hostname": "db-prod-01",
  "labels": {
    "env": "prod",
    "idc": "sh-1",
    "role": "mysql"
  },
  "status": "online",
  "platform": {
    "os": "linux",
    "arch": "amd64",
    "kernel": "6.6.0"
  },
  "agent": {
    "version": "0.1.0",
    "connected_via": ["proxy-sh-01"]
  },
  "last_seen_at": "2026-05-07T10:00:00Z",
  "registered_at": "2026-05-01T08:00:00Z"
}
```

字段约束：

- `id` 是主键
- `hostname` 用于展示和筛选，不保证永久不变
- `status` 枚举：`online`、`offline`、`degraded`
- `connected_via` 为从 center 到目标节点的转发路径摘要

### 3.2 Capability

```json
{
  "name": "fs.read",
  "type": "builtin",
  "risk_level": "readonly",
  "enabled": true
}
```

字段约束：

- `type` 枚举：`builtin`、`command_template`
- `risk_level` 枚举：`readonly`、`mutating`、`dangerous`

### 3.3 CommandTemplate

```json
{
  "id": "cmdtpl_01J0A2",
  "name": "docker.ps",
  "description": "List containers on target nodes",
  "risk_level": "readonly",
  "target_os": ["linux"],
  "executor": {
    "type": "process",
    "command": "/usr/bin/docker",
    "args": ["ps", "--format", "{{.Names}}\\t{{.Status}}"]
  },
  "params_schema": {
    "type": "object",
    "properties": {
      "all": { "type": "boolean" }
    },
    "additionalProperties": false
  },
  "default_timeout_sec": 10,
  "max_timeout_sec": 30,
  "max_output_bytes": 262144,
  "enabled": true,
  "created_by": "user_admin_01",
  "created_at": "2026-05-07T10:00:00Z",
  "updated_at": "2026-05-07T10:00:00Z"
}
```

字段约束：

- `name` 全局唯一，便于 CLI 与 MCP 直接引用
- `executor.type` 在 v1 仅支持 `process`
- `params_schema` 必填
- `additionalProperties` 建议默认关闭
- `enabled=false` 时禁止调用

### 3.4 Invocation

```json
{
  "id": "inv_01J0A3",
  "action": "fs.read",
  "action_type": "builtin",
  "status": "running",
  "async": true,
  "targets": {
    "node_ids": ["node_01J0A1"]
  },
  "params": {
    "path": "/etc/hostname"
  },
  "requested_by": {
    "subject_id": "user_ops_01",
    "source": "cli"
  },
  "timeout_sec": 10,
  "created_at": "2026-05-07T10:00:00Z",
  "started_at": "2026-05-07T10:00:01Z",
  "finished_at": null
}
```

字段约束：

- `action_type` 枚举：`builtin`、`command_template`
- `status` 枚举：`pending`、`running`、`succeeded`、`failed`、`partial`、`canceled`
- `targets` 在 v1 至少支持 `node_ids`

### 3.5 InvocationResult

```json
{
  "node_id": "node_01J0A1",
  "hostname": "db-prod-01",
  "status": "succeeded",
  "started_at": "2026-05-07T10:00:01Z",
  "finished_at": "2026-05-07T10:00:02Z",
  "data": {
    "content": "db-prod-01\n"
  },
  "error": null
}
```

失败时：

```json
{
  "node_id": "node_01J0A1",
  "hostname": "db-prod-01",
  "status": "failed",
  "started_at": "2026-05-07T10:00:01Z",
  "finished_at": "2026-05-07T10:00:02Z",
  "data": null,
  "error": {
    "code": "POLICY_DENIED",
    "message": "request exceeds runtime limits"
  }
}
```

---

## 四、错误模型

### 4.1 HTTP 状态码

建议映射：

- `200 OK`
- `201 Created`
- `202 Accepted`
- `400 Bad Request`
- `401 Unauthorized`
- `403 Forbidden`
- `404 Not Found`
- `409 Conflict`
- `422 Unprocessable Entity`
- `429 Too Many Requests`
- `500 Internal Server Error`
- `503 Service Unavailable`

### 4.2 业务错误体

```json
{
  "error": {
    "code": "NODE_OFFLINE",
    "message": "target node is offline",
    "request_id": "req_01J0A9",
    "details": {
      "node_id": "node_01J0A1"
    }
  }
}
```

### 4.3 推荐错误码

- `UNAUTHORIZED`
- `FORBIDDEN`
- `INVALID_ARGUMENT`
- `NODE_NOT_FOUND`
- `NODE_OFFLINE`
- `CAPABILITY_DISABLED`
- `COMMAND_TEMPLATE_DISABLED`
- `POLICY_DENIED`
- `TIMEOUT`
- `CANCELED`
- `RATE_LIMITED`
- `UPSTREAM_UNAVAILABLE`
- `INTERNAL_ERROR`

原则：

- 对外错误码稳定
- `message` 面向调用方可读
- `details` 只放结构化补充，不暴露内部堆栈

---

## 五、节点查询 API

### 5.1 `GET /v1/nodes`

用途：

- 列出节点
- 按标签与状态筛选
- 为 CLI、Web、MCP 提供节点发现能力

请求参数：

- `limit`
- `cursor`
- `hostname`
- `status`
- `label_selector`

`label_selector` 建议采用简化语法：

```text
env=prod,idc=sh-1
```

响应示例：

```json
{
  "items": [
    {
      "id": "node_01J0A1",
      "hostname": "db-prod-01",
      "labels": {
        "env": "prod",
        "idc": "sh-1"
      },
      "status": "online",
      "platform": {
        "os": "linux",
        "arch": "amd64",
        "kernel": "6.6.0"
      },
      "agent": {
        "version": "0.1.0",
        "connected_via": ["proxy-sh-01"]
      },
      "last_seen_at": "2026-05-07T10:00:00Z",
      "registered_at": "2026-05-01T08:00:00Z"
    }
  ],
  "next_cursor": ""
}
```

约束：

- 不返回完整硬件详情
- 不返回敏感策略正文
- 离线节点默认也可查询，便于诊断与审计

### 5.2 `GET /v1/nodes/{node_id}`

用途：

- 查询单节点详情

响应：

- 返回 `Node`
- 节点不存在时返回 `404 NODE_NOT_FOUND`

### 5.3 `GET /v1/nodes/{node_id}/capabilities`

响应示例：

```json
{
  "items": [
    {
      "name": "fs.list",
      "type": "builtin",
      "risk_level": "readonly",
      "enabled": true
    },
    {
      "name": "docker.ps",
      "type": "command_template",
      "risk_level": "readonly",
      "enabled": true
    }
  ]
}
```

语义：

- 返回该节点当前可暴露的能力视图
- 结果应同时受节点平台、模板启用状态和最小运行时限制影响

---

## 六、命令模板 API

### 6.1 `GET /v1/command-templates`

请求参数：

- `limit`
- `cursor`
- `name`
- `enabled`
- `risk_level`
- `target_os`

响应：

```json
{
  "items": [
    {
      "id": "cmdtpl_01J0A2",
      "name": "docker.ps",
      "description": "List containers on target nodes",
      "risk_level": "readonly",
      "target_os": ["linux"],
      "executor": {
        "type": "process",
        "command": "/usr/bin/docker",
        "args": ["ps", "--format", "{{.Names}}\\t{{.Status}}"]
      },
      "params_schema": {
        "type": "object",
        "properties": {
          "all": { "type": "boolean" }
        },
        "additionalProperties": false
      },
      "default_timeout_sec": 10,
      "max_timeout_sec": 30,
      "max_output_bytes": 262144,
      "enabled": true,
      "created_by": "user_admin_01",
      "created_at": "2026-05-07T10:00:00Z",
      "updated_at": "2026-05-07T10:00:00Z"
    }
  ],
  "next_cursor": ""
}
```

### 6.2 `POST /v1/command-templates`

用途：

- 创建命令模板

请求示例：

```json
{
  "name": "docker.ps",
  "description": "List containers on target nodes",
  "risk_level": "readonly",
  "target_os": ["linux"],
  "executor": {
    "type": "process",
    "command": "/usr/bin/docker",
    "args": ["ps", "--format", "{{.Names}}\\t{{.Status}}"]
  },
  "params_schema": {
    "type": "object",
    "properties": {
      "all": { "type": "boolean" }
    },
    "additionalProperties": false
  },
  "default_timeout_sec": 10,
  "max_timeout_sec": 30,
  "max_output_bytes": 262144
}
```

验证要求：

- `name` 唯一
- `executor.command` 必填且必须为显式可执行路径
- `params_schema.type` 必须为 `object`
- `default_timeout_sec <= max_timeout_sec`
- 高风险模板仅允许 `admin token` 创建

成功响应：

- `201 Created`
- 返回创建后的 `CommandTemplate`

### 6.3 `GET /v1/command-templates/{template_id}`

用途：

- 查询单个模板详情

### 6.4 `PATCH /v1/command-templates/{template_id}`

用途：

- 更新模板元数据
- 启用或停用模板

建议仅支持以下字段局部更新：

- `description`
- `risk_level`
- `target_os`
- `params_schema`
- `default_timeout_sec`
- `max_timeout_sec`
- `max_output_bytes`
- `enabled`

说明：

- 是否允许修改 `executor`，取决于治理要求
- 若允许修改，建议按“新版本模板”处理，而不是静默覆盖

### 6.5 `POST /v1/command-templates/{template_id}:invoke`

用途：

- 调用模板

请求示例：

```json
{
  "targets": {
    "node_ids": ["node_01J0A1", "node_01J0A2"]
  },
  "params": {
    "all": true
  },
  "timeout_sec": 10,
  "async": true
}
```

响应规则：

- `async=true` 时返回 `202 Accepted` 与 `Invocation`
- `async=false` 时返回 `200 OK` 与结果集合

---

## 七、统一执行 API

### 7.1 `POST /v1/invocations`

用途：

- 统一执行内置动作与命令模板
- 支持批量节点调用
- 为后续审计、取消、状态查询提供统一主键

请求示例：读取文件

```json
{
  "action": "fs.read",
  "action_type": "builtin",
  "targets": {
    "node_ids": ["node_01J0A1"]
  },
  "params": {
    "path": "/etc/hostname",
    "offset": 0,
    "length": 4096
  },
  "timeout_sec": 10,
  "async": false
}
```

请求示例：调用模板

```json
{
  "action": "docker.ps",
  "action_type": "command_template",
  "targets": {
    "node_ids": ["node_01J0A1", "node_01J0A2"]
  },
  "params": {
    "all": true
  },
  "timeout_sec": 10,
  "async": true
}
```

字段约束：

- `action` 必填
- `action_type` 必填
- `targets.node_ids` 在 v1 必填且不能为空
- `timeout_sec` 可选，未传时使用动作默认值
- `params` 必须满足该动作或模板对应 schema

同步成功响应示例：

```json
{
  "invocation": {
    "id": "inv_01J0A3",
    "action": "fs.read",
    "action_type": "builtin",
    "status": "succeeded",
    "async": false,
    "targets": {
      "node_ids": ["node_01J0A1"]
    },
    "params": {
      "path": "/etc/hostname",
      "offset": 0,
      "length": 4096
    },
    "requested_by": {
      "subject_id": "user_ops_01",
      "source": "cli"
    },
    "timeout_sec": 10,
    "created_at": "2026-05-07T10:00:00Z",
    "started_at": "2026-05-07T10:00:01Z",
    "finished_at": "2026-05-07T10:00:02Z"
  },
  "results": [
    {
      "node_id": "node_01J0A1",
      "hostname": "db-prod-01",
      "status": "succeeded",
      "started_at": "2026-05-07T10:00:01Z",
      "finished_at": "2026-05-07T10:00:02Z",
      "data": {
        "content": "db-prod-01\n"
      },
      "error": null
    }
  ]
}
```

异步成功响应示例：

```json
{
  "invocation": {
    "id": "inv_01J0A3",
    "action": "docker.ps",
    "action_type": "command_template",
    "status": "pending",
    "async": true,
    "targets": {
      "node_ids": ["node_01J0A1", "node_01J0A2"]
    },
    "params": {
      "all": true
    },
    "requested_by": {
      "subject_id": "user_ops_01",
      "source": "cli"
    },
    "timeout_sec": 10,
    "created_at": "2026-05-07T10:00:00Z",
    "started_at": null,
    "finished_at": null
  }
}
```

### 7.2 `GET /v1/invocations/{invocation_id}`

用途：

- 查询一次调用的元数据与整体状态

响应：

- 返回 `Invocation`

### 7.3 `GET /v1/invocations/{invocation_id}/results`

用途：

- 查询分节点执行结果

响应示例：

```json
{
  "items": [
    {
      "node_id": "node_01J0A1",
      "hostname": "db-prod-01",
      "status": "succeeded",
      "started_at": "2026-05-07T10:00:01Z",
      "finished_at": "2026-05-07T10:00:02Z",
      "data": {
        "stdout": "container-a\tUp 2h\n"
      },
      "error": null
    },
    {
      "node_id": "node_01J0A2",
      "hostname": "db-prod-02",
      "status": "failed",
      "started_at": "2026-05-07T10:00:01Z",
      "finished_at": "2026-05-07T10:00:02Z",
      "data": null,
      "error": {
        "code": "NODE_OFFLINE",
        "message": "target node is offline"
      }
    }
  ]
}
```

语义：

- 批量操作允许部分成功
- `Invocation.status=partial` 时，调用方应结合结果列表逐节点处理

### 7.4 `POST /v1/invocations/{invocation_id}:cancel`

用途：

- 取消一个仍处于 `pending` 或 `running` 的调用

响应示例：

```json
{
  "invocation": {
    "id": "inv_01J0A3",
    "status": "canceled",
    "finished_at": "2026-05-07T10:00:05Z"
  }
}
```

语义要求：

- 若调用已结束，返回当前最终状态
- center 应尽力向 agent 发送取消信号
- agent 若已完成，可忽略取消

---

## 八、内置动作 schema 约定

为避免 v1 过度发散，内置动作建议先固定最小集合，并为每个动作维护明确 schema。

### 8.1 `fs.list`

请求：

```json
{
  "path": "/var/log",
  "limit": 100
}
```

响应 `data`：

```json
{
  "entries": [
    {
      "name": "syslog",
      "type": "file",
      "size": 12345,
      "mode": "0644",
      "modified_at": "2026-05-07T09:59:00Z"
    }
  ]
}
```

约束：

- `path` 必填
- `limit` 可选，默认 100，最大 1000

### 8.2 `fs.read`

请求：

```json
{
  "path": "/etc/hostname",
  "offset": 0,
  "length": 4096
}
```

响应 `data`：

```json
{
  "content": "db-prod-01\n",
  "encoding": "utf-8",
  "truncated": false
}
```

约束：

- `path` 必填
- `offset` 默认 `0`
- `length` 默认 `4096`
- `length` 不得超过策略上限

### 8.3 `fs.stat`

请求：

```json
{
  "path": "/etc/hostname"
}
```

响应 `data`：

```json
{
  "name": "hostname",
  "type": "file",
  "size": 12,
  "mode": "0644",
  "modified_at": "2026-05-07T09:59:00Z"
}
```

### 8.4 `fs.write`

请求：

```json
{
  "path": "/tmp/demo.txt",
  "content": "hello\n",
  "encoding": "utf-8",
  "overwrite": true
}
```

说明：

- 默认高风险
- 若 token 类型不允许或运行时限制拒绝，应直接返回 `POLICY_DENIED` 或 `FORBIDDEN`

### 8.5 `sys.info`

请求：

```json
{}
```

响应 `data`：

```json
{
  "hostname": "db-prod-01",
  "os": "linux",
  "arch": "amd64",
  "kernel": "6.6.0",
  "uptime_sec": 7200
}
```

---

## 九、token 鉴权与审计挂点

v1 虽不完整定义安全模型实现，但接口层必须预留明确语义。

### 9.1 token 判定点

一期不引入 RBAC，至少按以下步骤执行 token 鉴权与动作控制：

1. 是否识别并接受该 token
2. 该 token 类型是否允许访问目标 API
3. 该 token 类型是否允许执行目标动作
4. 若为高风险操作，是否要求 `admin token`

### 9.2 审计字段

所有执行类请求至少记录：

- `request_id`
- `invocation_id`
- `subject_id`
- `source`
- `action`
- `action_type`
- `target_node_ids`
- `risk_level`
- `decision`
- `started_at`
- `finished_at`

---

## 十、MCP 与 CLI 适配建议

### 10.1 CLI

CLI 可以直接映射到 API：

```text
sysplane nodes list
sysplane nodes get <node-id>
sysplane fs read --node <node-id> --path /etc/hostname
sysplane commands invoke docker.ps --node <node-id> --all
sysplane invocations get <invocation-id>
```

### 10.2 MCP

MCP 不应直接暴露全部 REST 资源，而应投影为高频工具：

- `list_nodes`
- `read_file`
- `list_directory`
- `get_system_info`
- `run_command_template`

每个 MCP tool 内部仍调用统一 API，并把返回结果映射回：

- `node_id`
- `invocation_id`
- `status`
- `error.code`

---

## 十一、实现建议

center 落地 API v1 时，建议内部按以下边界组织：

- `node service`
- `command template service`
- `invocation service`
- `auth service`
- `audit service`

不建议：

- 直接让 HTTP handler 拼装全部业务逻辑
- 在 proxy 中实现任何 API 语义
- 将 MCP tool 逻辑绕过 API service 直接操作底层路由

---

## 十二、后续扩展点

v1 之后可继续扩展：

1. `target selectors`
   - 支持 `label_selector`
   - 支持节点组
2. `streaming`
   - 大文件分块读取
   - 长输出流式返回
3. `approval`
   - 高风险动作审批
4. `audit API`
   - 更细粒度检索与导出

---

## 十三、文档状态

| 项目 | 说明 |
| ---- | ---- |
| 状态 | API v1 草案 |
| 依赖文档 | `docs/design/sysplane-vision.md` |
| 当前范围 | 节点、命令模板、统一执行 |
