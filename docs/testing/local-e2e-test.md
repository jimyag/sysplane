# 本地端到端测试流程

本文档记录 Sysplane 在单台开发机上运行 center / proxy / agent，并通过 HTTP API 与 WebUI 做本地联调的最小流程。

---

## 编译

```bash
cd /path/to/sys-mcp
task build
```

---

## 创建测试配置

```bash
mkdir -p /tmp/sysplane-test
```

### center.yaml

```yaml
listen:
  http_address: ":18880"
  grpc_address: ":18890"

auth:
  client_tokens:
    - "test-client-token"
  admin_tokens:
    - "test-admin-token"
  agent_tokens:
    - "test-agent-token"
  proxy_tokens:
    - "test-proxy-token"

router:
  request_timeout_sec: 10

logging:
  level: "debug"
  format: "json"
```

### agent-direct.yaml

```yaml
hostname: "agent-direct"
upstream:
  address: "127.0.0.1:18890"
  token: "test-agent-token"
tool_timeout_sec: 25
security:
  max_file_size_mb: 10
  blocked_paths:
    - /proc
    - /sys
    - /dev
  allowed_commands:
    - /bin/echo
logging:
  level: "info"
  format: "json"
```

### proxy.yaml

```yaml
hostname: "proxy-idc1"
listen:
  grpc_address: ":18892"
upstream:
  address: "127.0.0.1:18890"
  token: "test-proxy-token"
auth:
  agent_tokens:
    - "test-proxy-agent-token"
  proxy_tokens:
    - "test-proxy-downstream-token"
logging:
  level: "info"
  format: "json"
```

### agent-behind-proxy.yaml

```yaml
hostname: "agent-behind-proxy"
upstream:
  address: "127.0.0.1:18892"
  token: "test-proxy-agent-token"
tool_timeout_sec: 25
security:
  max_file_size_mb: 10
  blocked_paths:
    - /proc
    - /sys
    - /dev
  allowed_commands:
    - /bin/echo
logging:
  level: "info"
  format: "json"
```

---

## 启动服务

```bash
./bin/sysplane-center -config /tmp/sysplane-test/center.yaml
./bin/sysplane-agent -config /tmp/sysplane-test/agent-direct.yaml
./bin/sysplane-proxy -config /tmp/sysplane-test/proxy.yaml
./bin/sysplane-agent -config /tmp/sysplane-test/agent-behind-proxy.yaml
```

---

## 验证 HTTP API

列出节点：

```bash
curl -H 'Authorization: Bearer test-client-token' \
  http://127.0.0.1:18880/v1/nodes
```

查看能力：

```bash
curl -H 'Authorization: Bearer test-client-token' \
  http://127.0.0.1:18880/v1/nodes/agent-direct/capabilities
```

执行硬件信息动作：

```bash
curl -X POST \
  -H 'Authorization: Bearer test-client-token' \
  -H 'Content-Type: application/json' \
  http://127.0.0.1:18880/v1/nodes/agent-direct/actions/sys:hardware
```

---

## 验证命令模板与 Invocation

创建模板：

```bash
curl -X POST \
  -H 'Authorization: Bearer test-admin-token' \
  -H 'Content-Type: application/json' \
  http://127.0.0.1:18880/v1/command-templates \
  -d '{
    "name":"echo.hello",
    "description":"Echo a fixed string",
    "risk_level":"readonly",
    "target_os":["linux"],
    "executor":{"type":"process","command":"/bin/echo","args":["hello"]},
    "params_schema":{"type":"object","properties":{},"additionalProperties":false},
    "default_timeout_sec":10,
    "max_timeout_sec":30,
    "max_output_bytes":4096
  }'
```

调用模板：

```bash
curl -X POST \
  -H 'Authorization: Bearer test-admin-token' \
  -H 'Content-Type: application/json' \
  http://127.0.0.1:18880/v1/command-templates/<template-id>:invoke \
  -d '{
    "targets":{"node_ids":["agent-direct"]},
    "params":{},
    "async":false
  }'
```

统一执行 builtin：

```bash
curl -X POST \
  -H 'Authorization: Bearer test-client-token' \
  -H 'Content-Type: application/json' \
  http://127.0.0.1:18880/v1/invocations \
  -d '{
    "action":"fs.read",
    "action_type":"builtin",
    "targets":{"node_ids":["agent-direct"]},
    "params":{"path":"/etc/hostname"},
    "async":false
  }'
```

查询审计：

```bash
curl -H 'Authorization: Bearer test-admin-token' \
  http://127.0.0.1:18880/v1/audit/events
```

---

## 验证 WebUI

浏览器打开：

```text
http://127.0.0.1:18880/web/
```

输入 `test-admin-token` 或 `test-client-token` 后，应能看到：

- 节点列表、详情、能力与快捷动作
- 命令模板管理页面
- Invocation 执行中心
- 审计页面（`admin token`）
