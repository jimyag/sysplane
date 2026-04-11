package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	clientcfg "github.com/jimyag/sys-mcp/internal/sys-mcp-client/config"
)

func Run(ctx context.Context, configPath string) error {
	cfg, err := clientcfg.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if fi, err := os.Stat(configPath); err == nil && fi.Mode().Perm()&0o044 != 0 {
		fmt.Fprintf(os.Stderr, "warning: %s is group/world readable; consider chmod 0600\n", configPath)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.Logging.Level),
	}))

	httpClient := &http.Client{
		Transport: &authTransport{
			base:  http.DefaultTransport,
			token: cfg.Center.Token,
		},
	}

	upstreamClient := mcp.NewClient(&mcp.Implementation{
		Name:    "sys-mcp-client",
		Version: "1.0.0",
	}, nil)

	sseTransport := &mcp.SSEClientTransport{
		Endpoint:   cfg.Center.URL + "/sse",
		HTTPClient: httpClient,
	}

	cs, err := upstreamClient.Connect(ctx, sseTransport, nil)
	if err != nil {
		return fmt.Errorf("connect to center %s: %w", cfg.Center.URL, err)
	}
	defer cs.Close()

	logger.Info("connected to center", "url", cfg.Center.URL)

	toolsResult, err := cs.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	logger.Info("discovered tools", "count", len(toolsResult.Tools))

	localSrv := mcp.NewServer(&mcp.Implementation{
		Name:    "sys-mcp-client",
		Version: "1.0.0",
	}, nil)

	for _, t := range toolsResult.Tools {
		tool := t
		localSrv.AddTool(&mcp.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		}, func(reqCtx context.Context, req *mcp.ServerRequest[*mcp.CallToolParamsRaw]) (*mcp.CallToolResult, error) {
			var args any
			if req.Params.Arguments != nil {
				if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
					args = nil
				}
			}
			return cs.CallTool(reqCtx, &mcp.CallToolParams{
				Name:      tool.Name,
				Arguments: args,
			})
		})
	}

	logger.Info("starting stdio MCP server")
	if err := localSrv.Run(ctx, &mcp.StdioTransport{}); err != nil && err != context.Canceled {
		return fmt.Errorf("stdio server: %w", err)
	}
	return nil
}

type authTransport struct {
	base  http.RoundTripper
	token string
}

func (a *authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.Header.Set("Authorization", "Bearer "+a.token)
	return a.base.RoundTrip(clone)
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
