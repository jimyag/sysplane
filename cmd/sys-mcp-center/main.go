package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/jimyag/sys-mcp/api/tunnel"
	centercfg "github.com/jimyag/sys-mcp/internal/sys-mcp-center/config"
	"github.com/jimyag/sys-mcp/internal/sys-mcp-center"
	centermcp "github.com/jimyag/sys-mcp/internal/sys-mcp-center/mcp"
	"github.com/jimyag/sys-mcp/internal/sys-mcp-center/registry"
	"github.com/jimyag/sys-mcp/internal/sys-mcp-center/router"
)

var defaultConfigPaths = []string{
	"./sys-mcp-center.yaml",
	"/etc/sys-mcp/center.yaml",
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "path to center config file")
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

	cfg, err := centercfg.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load config: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.Logging.Level),
	}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	reg := registry.New()
	reg.StartOfflineChecker(ctx, 90*time.Second)

	rtr := router.New(cfg.Router.RequestTimeoutSec)

	tunnelSvc := center.NewTunnelServiceServer(reg, rtr, cfg.Auth.AgentTokens, logger)

	// gRPC server (no TLS for Phase 1 MVP; add later via cfg.Listen.TLS).
	grpcServer := grpc.NewServer(grpc.Creds(insecure.NewCredentials()))
	tunnel.RegisterTunnelServiceServer(grpcServer, tunnelSvc)

	mcpHandler := centermcp.NewMCPHandler(reg, rtr, cfg.Auth.ClientTokens)

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		lis, err := net.Listen("tcp", cfg.Listen.GRPCAddress)
		if err != nil {
			return fmt.Errorf("gRPC listen %s: %w", cfg.Listen.GRPCAddress, err)
		}
		logger.Info("gRPC server listening", "address", cfg.Listen.GRPCAddress)
		go func() {
			<-gCtx.Done()
			grpcServer.GracefulStop()
		}()
		return grpcServer.Serve(lis)
	})

	g.Go(func() error {
		httpSrv := &http.Server{
			Addr:    cfg.Listen.HTTPAddress,
			Handler: mcpHandler,
		}
		logger.Info("HTTP/MCP server listening", "address", cfg.Listen.HTTPAddress)
		go func() {
			<-gCtx.Done()
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = httpSrv.Shutdown(shutCtx)
		}()
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("HTTP server: %w", err)
		}
		return nil
	})

	if err := g.Wait(); err != nil && err != context.Canceled {
		logger.Error("center exited with error", "error", err)
		os.Exit(1)
	}
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
