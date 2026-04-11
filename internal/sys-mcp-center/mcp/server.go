package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jimyag/sys-mcp/internal/pkg/stream"
	"github.com/jimyag/sys-mcp/internal/sys-mcp-center/registry"
	"github.com/jimyag/sys-mcp/internal/sys-mcp-center/router"
)

// emptySchema is used for tools that take no parameters beyond target_host.
var emptySchema = &jsonschema.Schema{Type: "object"}

// NewMCPHandler builds the MCP HTTP handler for center.
// It wraps the MCP SSE endpoint with bearer token auth.
func NewMCPHandler(reg *registry.Registry, rtr *router.Router, clientTokens []string) http.Handler {
	srv := buildServer(reg, rtr)
	handler := sdkmcp.NewSSEHandler(func(*http.Request) *sdkmcp.Server { return srv }, nil)
	return BearerTokenMiddleware(clientTokens, handler)
}

func buildServer(reg *registry.Registry, rtr *router.Router) *sdkmcp.Server {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "sys-mcp-center",
		Version: "0.1.0",
	}, &sdkmcp.ServerOptions{
		Instructions: "Query remote physical machine resources via registered agents.",
	})

	// list_agents: center-local tool.
	registerListAgents(srv, reg)

	// 7 agent proxy tools.
	for _, toolName := range agentToolNames {
		registerAgentProxyTool(srv, reg, rtr, toolName)
	}

	// 多机并发工具
	registerMultiTool(srv, reg, rtr, "get_hardware_info_multi")
	registerMultiTool(srv, reg, rtr, "list_directory_multi")

	return srv
}

var agentToolNames = []string{
	"list_directory",
	"stat_file",
	"check_path_exists",
	"read_file",
	"search_file_content",
	"get_hardware_info",
	"proxy_local_api",
}

// listAgentsResult is the return shape for list_agents.
type listAgentsResult struct {
	Agents []agentInfo `json:"agents"`
	Total  int         `json:"total"`
	Online int         `json:"online"`
}

type agentInfo struct {
	Hostname     string   `json:"hostname"`
	IP           string   `json:"ip"`
	OS           string   `json:"os"`
	NodeType     string   `json:"node_type"`
	Status       string   `json:"status"`
	ProxyPath    []string `json:"proxy_path"`
	AgentVersion string   `json:"agent_version"`
}

func registerListAgents(srv *sdkmcp.Server, reg *registry.Registry) {
	tool := &sdkmcp.Tool{
		Name:        "list_agents",
		Description: "List all registered agents and proxies with their online status.",
		InputSchema: emptySchema,
	}
	srv.AddTool(tool, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		records := reg.All()
		agents := make([]agentInfo, 0, len(records))
		for _, r := range records {
			agents = append(agents, agentInfo{
				Hostname:     r.Hostname,
				IP:           r.IP,
				OS:           r.OS,
				NodeType:     r.NodeType,
				Status:       string(r.Status),
				ProxyPath:    r.ProxyPath,
				AgentVersion: r.AgentVersion,
			})
		}
		res := listAgentsResult{
			Agents: agents,
			Total:  len(agents),
			Online: reg.OnlineCount(),
		}
		b, _ := json.Marshal(res)
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: string(b)},
			},
		}, nil
	})
}

func registerAgentProxyTool(srv *sdkmcp.Server, reg *registry.Registry, rtr *router.Router, toolName string) {
	tool := &sdkmcp.Tool{
		Name:        toolName,
		Description: agentToolDescription(toolName),
		InputSchema: emptySchema,
	}
	srv.AddTool(tool, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		argsRaw := req.Params.Arguments
		if argsRaw == nil {
			return errorResult("missing arguments"), nil
		}

		// Extract target_host from the args.
		var base map[string]json.RawMessage
		if err := json.Unmarshal(argsRaw, &base); err != nil {
			return errorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
		}
		targetHostRaw, ok := base["target_host"]
		if !ok {
			return errorResult("target_host is required"), nil
		}
		var targetHost string
		if err := json.Unmarshal(targetHostRaw, &targetHost); err != nil || targetHost == "" {
			return errorResult("target_host must be a non-empty string"), nil
		}

		rec := reg.Lookup(targetHost)
		if rec == nil {
			return errorResult(fmt.Sprintf("agent %q not found", targetHost)), nil
		}
		if rec.Status != registry.StatusOnline {
			return errorResult(fmt.Sprintf("agent %q is offline", targetHost)), nil
		}

		requestID := stream.NewRequestID(toolName)
		result, err := rtr.Send(ctx, rec, requestID, toolName, string(argsRaw))
		if err != nil {
			return errorResult(err.Error()), nil
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: result},
			},
		}, nil
	})
}

func errorResult(msg string) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{
		IsError: true,
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: msg}},
	}
}

func agentToolDescription(name string) string {
	switch name {
	case "list_directory":
		return "List contents of a directory on a remote agent. Requires target_host."
	case "stat_file":
		return "Get metadata (size, permissions, type) of a file on a remote agent. Requires target_host."
	case "check_path_exists":
		return "Check whether a path exists on a remote agent. Requires target_host."
	case "read_file":
		return "Read the contents of a file on a remote agent. Requires target_host."
	case "search_file_content":
		return "Search file contents with pattern matching on a remote agent. Requires target_host."
	case "get_hardware_info":
		return "Get CPU, memory, disk and network info from a remote agent. Requires target_host."
	case "proxy_local_api":
		return "Call a local HTTP API on a remote agent host. Requires target_host."
	default:
		return fmt.Sprintf("Proxy tool %s. Requires target_host.", name)
	}
}

// multiToolSchema 定义多机工具的输入 schema：包含 target_hosts 数组和可选的 args。
var multiToolSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"target_hosts": {
			Type:        "array",
			Description: "目标主机名列表，留空表示查询所有在线主机",
			Items:       &jsonschema.Schema{Type: "string"},
		},
		"args": {
			Type:        "object",
			Description: "透传给底层工具的参数（与单机工具相同）",
		},
	},
}

// registerMultiTool 注册一个多机并发工具，工具名约定为 <singleToolName>_multi。
// 底层调用对应的单机工具名（去掉 _multi 后缀）。
func registerMultiTool(srv *sdkmcp.Server, reg *registry.Registry, rtr *router.Router, toolName string) {
	singleTool := strings.TrimSuffix(toolName, "_multi")

	tool := &sdkmcp.Tool{
		Name:        toolName,
		Description: fmt.Sprintf("在多台 agent 上并发执行 %s，返回各主机的结果 map。", singleTool),
		InputSchema: multiToolSchema,
	}

	srv.AddTool(tool, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var params struct {
			TargetHosts []string        `json:"target_hosts"`
			Args        json.RawMessage `json:"args"`
		}
		if req.Params.Arguments != nil {
			if err := json.Unmarshal(req.Params.Arguments, &params); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
		}

		// 决定目标列表
		var records []*registry.AgentRecord
		if len(params.TargetHosts) == 0 {
			for _, r := range reg.All() {
				if r.Status == registry.StatusOnline {
					records = append(records, r)
				}
			}
		} else {
			for _, h := range params.TargetHosts {
				r := reg.Lookup(h)
				if r == nil {
					return nil, fmt.Errorf("agent not found: %s", h)
				}
				records = append(records, r)
			}
		}

		if len(records) == 0 {
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: `{"agents":[],"message":"没有在线的 agent"}`}},
			}, nil
		}

		argsJSON := "{}"
		if params.Args != nil {
			argsJSON = string(params.Args)
		}

		requestIDBase := fmt.Sprintf("multi-%s-%d", singleTool, time.Now().UnixNano())
		multiResults := rtr.SendMulti(ctx, records, requestIDBase, singleTool, argsJSON)

		// 聚合结果为 map
		resultMap := make(map[string]interface{}, len(multiResults))
		for _, mr := range multiResults {
			if mr.Err != nil {
				resultMap[mr.Hostname] = map[string]string{"error": mr.Err.Error()}
			} else {
				var parsed interface{}
				if err := json.Unmarshal([]byte(mr.Result), &parsed); err != nil {
					resultMap[mr.Hostname] = mr.Result
				} else {
					resultMap[mr.Hostname] = parsed
				}
			}
		}

		b, _ := json.Marshal(resultMap)
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(b)}},
		}, nil
	})
}

