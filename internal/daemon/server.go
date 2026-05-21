// Package daemon — RPC server + metrics HTTP.
package daemon

import (
	"azcontainer/internal/log"
	"azcontainer/internal/metrics"
	"fmt"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

const MetricsAddr = ":9090"

var logger = log.Module("daemon")

// Serve — daemon-u işə salır.
func Serve() error {
	log.ConfigureFromEnv()

	if err := os.MkdirAll(filepath.Dir(Socket), 0755); err != nil {
		return fmt.Errorf("socket dir: %w", err)
	}
	_ = os.Remove(Socket)

	api, err := NewAPI()
	if err != nil {
		return fmt.Errorf("api: %w", err)
	}

	server := rpc.NewServer()
	if err := server.Register(api); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	listener, err := net.Listen("unix", Socket)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	if err := os.Chmod(Socket, 0666); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	logger.Info("daemon başladı", "socket", Socket, "metrics", MetricsAddr)

	// Metrics HTTP-i background-da işə sal.
	go func() {
		if err := metrics.ServeMetrics(MetricsAddr); err != nil {
			logger.Warn("metrics server", "err", err)
		}
	}()

	// Graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("dayandırılır", "signal", sig)
		listener.Close()
		os.Remove(Socket)
		os.Exit(0)
	}()

	// Accept loop.
	for {
		conn, err := listener.Accept()
		if err != nil {
			return nil
		}
		go func(c net.Conn) {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("connection panic", "err", fmt.Sprintf("%v", r))
				}
				c.Close()
			}()
			server.ServeCodec(jsonrpc.NewServerCodec(c))
		}(conn)
	}
}
