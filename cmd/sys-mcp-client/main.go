// sys-mcp-client is a stdio MCP bridge that connects an AI assistant to
// sys-mcp-center. It discovers available tools from center and exposes them
// locally via stdio so the AI can call them transparently.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	clientcfg "github.com/jimyag/sys-mcp/internal/sys-mcp-client/config"
)

var defaultConfigPaths = []string{
	"./sys-mcp-client.yaml",
	os.Getenv("HOME") + "/.config/sys-mcp/client.yaml",
	"/etc/sys-mcp/client.yaml",
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "path to client config file")
	flag.Parse()

	if configPath == "" {
		for _, p := range defaultConfigPaths {
			if p == "" {
				continue
			}
			if _, err := os.Stat(p); err == nil {
				configPath = p
				break
			}
		}
	}
	if configPath == "" {
		fmt.Fprintln(os.Stderr, "error: no config file found; use --config")
		os.Exit(1)
	}

	cfg, err := clientcfg.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load config: %v\n", err)
		os.Exit(1)
	}

	// Warn if the config file is world-readable (contains a token).
	if fi, err := os.Stat(configPath); err == nil {
		if fi.Mode().Perm()&0o044 != 0 {
			fmt.Fprintf(os.Stderr, "warning: %s is group/world readable; consider chmod 0600\n", configPath)
		}
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.Logging.Level),
	}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Build an HTTP client that injects the bearer token on every request.
	httpClient := &http.Client{
		Transport: &authTransport{
			base:  http.DefaultTransport,
			token: cfg.Center.Token,
		},
	}

	// Connect to sys-mcp-center via SSE transport.
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
		fmt.Fprintf(os.Stderr, "error: connect to center %s: %v\n", cfg.Center.URL, err)
		os.Exit(1)
	}
	defer cs.Close()

	logger.Info("connected to center", "url", cfg.Center.URL)

	// Discover tools from center.
	toolsResult, err := cs.ListTools(ctx, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: list tools: %v\n", err)
		os.Exit(1)
	}
	logger.Info("discovered tools", "count", len(toolsResult.Tools))

	// Build a local stdio MCP server that forwards every call to center.
	localSrv := mcp.NewServer(&mcp.Implementation{
		Name:    "sys-mcp-client",
		Version: "1.0.0",
	}, nil)

	for _, t := range toolsResult.Tools {
		tool := t // capture loop variable
		localSrv.AddTool(&mcp.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		}, func(reqCtx context.Context, req *mcp.ServerRequest[*mcp.CallToolParamsRaw]) (*mcp.CallToolResult, error) {
			// Forward raw args to center as-is.
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

	// Run the local server on stdio so the AI assistant can talk to us.
	logger.Info("starting stdio MCP server")
	if err := localSrv.Run(ctx, &mcp.StdioTransport{}); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "error: stdio server: %v\n", err)
		os.Exit(1)
	}
}

// authTransport adds an Authorization: Bearer header to every request.
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
