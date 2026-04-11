// sys-mcp-proxy aggregates multiple agents within an IDC and relays their
// tool requests/responses to sys-mcp-center.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	apitunnel "github.com/jimyag/sys-mcp/api/tunnel"
	pkgstream "github.com/jimyag/sys-mcp/internal/pkg/stream"
	proxycfg "github.com/jimyag/sys-mcp/internal/sys-mcp-proxy/config"
	proxyreg "github.com/jimyag/sys-mcp/internal/sys-mcp-proxy/registry"
	proxytunnel "github.com/jimyag/sys-mcp/internal/sys-mcp-proxy/tunnel"
)

var defaultConfigPaths = []string{
	"./sys-mcp-proxy.yaml",
	"/etc/sys-mcp/proxy.yaml",
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "path to proxy config file")
	flag.Parse()

	if configPath == "" {
		for _, p := range defaultConfigPaths {
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

	cfg, err := proxycfg.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load config: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.Logging.Level),
	}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Resolve proxy hostname for proxy_path labelling.
	proxyHostname := cfg.Hostname
	if proxyHostname == "" {
		proxyHostname, _ = os.Hostname()
	}
	if proxyHostname == "" {
		proxyHostname = "proxy"
	}

	reg := proxyreg.New()
	reg.StartOfflineChecker(ctx, 90*time.Second)

	// upstreamSend is used before the dialer is wired; replaced after init.
	// We use an adapter struct that holds a pointer-to-Dialer so we can swap it.
	dialerHolder := &dialerAdapter{}

	downstreamSvc := proxytunnel.NewDownstreamService(
		reg,
		cfg.Auth.AgentTokens,
		dialerHolder,
		proxyHostname,
		logger,
	)

	// Build dialer config: register this node as a PROXY on the upstream.
	dialerCfg := pkgstream.DialerConfig{
		Endpoint: cfg.Upstream.Address,
		RegisterMsg: &apitunnel.TunnelMessage{
			Payload: &apitunnel.TunnelMessage_RegisterRequest{
				RegisterRequest: &apitunnel.RegisterRequest{
					Hostname:     proxyHostname,
					Os:           "proxy",
					AgentVersion: "1.0.0",
					NodeType:     apitunnel.NodeType_NODE_TYPE_PROXY,
					Token:        cfg.Upstream.Token,
				},
			},
		},
		HeartbeatInterval: 30 * time.Second,
		ReconnectMaxDelay: 30 * time.Second,
		OnMessage: func(msg *apitunnel.TunnelMessage) {
			// Messages from upstream: route ToolRequests to downstream agents.
			switch msg.Payload.(type) {
			case *apitunnel.TunnelMessage_ToolRequest:
				downstreamSvc.DeliverToolRequest(msg)
			default:
				logger.Warn("proxy received unexpected upstream message", "type", fmt.Sprintf("%T", msg.Payload))
			}
		},
		OnRegisterAck: func(ack *apitunnel.RegisterAck) {
			if !ack.Success {
				logger.Error("upstream rejected registration", "message", ack.Message)
				return
			}
			logger.Info("registered with upstream", "address", cfg.Upstream.Address)
			// Re-register all currently known downstream agents so center has them.
			downstreamSvc.ReregisterAll(ctx)
		},
	}

	dialer := pkgstream.NewDialer(dialerCfg)
	dialerHolder.d = dialer

	g, gCtx := errgroup.WithContext(ctx)

	// Run upstream dialer.
	g.Go(func() error {
		return dialer.Run(gCtx)
	})

	// Run downstream gRPC server.
	g.Go(func() error {
		grpcServer := grpc.NewServer(grpc.Creds(insecure.NewCredentials()))
		apitunnel.RegisterTunnelServiceServer(grpcServer, downstreamSvc)

		lis, err := net.Listen("tcp", cfg.Listen.GRPCAddress)
		if err != nil {
			return fmt.Errorf("downstream listen %s: %w", cfg.Listen.GRPCAddress, err)
		}
		logger.Info("downstream gRPC listening", "address", cfg.Listen.GRPCAddress)
		go func() {
			<-gCtx.Done()
			grpcServer.GracefulStop()
		}()
		return grpcServer.Serve(lis)
	})

	if err := g.Wait(); err != nil && err != context.Canceled {
		logger.Error("proxy exited with error", "error", err)
		os.Exit(1)
	}
}

// dialerAdapter implements the Upstream interface so DownstreamService can
// send messages before the Dialer is fully constructed.
type dialerAdapter struct {
	d *pkgstream.Dialer
}

func (a *dialerAdapter) Send(msg *apitunnel.TunnelMessage) error {
	if a.d == nil {
		return fmt.Errorf("proxy: upstream dialer not ready")
	}
	return a.d.Send(msg)
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
