package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"talka/internal/app"
	"talka/internal/config"
	"talka/internal/mdns"
)

var newDiscoveryPublisher = mdns.NewSystemPublisher

type discoveryEvidence struct {
	Descriptor mdns.Descriptor
	Port       int
}

func buildDiscoveryEvidence(serviceName string, port int) (discoveryEvidence, error) {
	if port <= 0 {
		return discoveryEvidence{}, fmt.Errorf("listener port must be greater than zero")
	}

	desc, err := mdns.NewDescriptor(serviceName, mdns.PairingRequired)
	if err != nil {
		return discoveryEvidence{}, err
	}
	if err := mdns.ValidateTXT(desc.TXTRecords()); err != nil {
		return discoveryEvidence{}, err
	}

	return discoveryEvidence{Descriptor: desc, Port: port}, nil
}

func run(ctx context.Context, stdout, stderr io.Writer, args []string) error {
	fs := flag.NewFlagSet("talka-server", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "path to Talka config YAML")
	listenAddr := fs.String("listen", "", "control API listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home directory: %w", err)
		}
		*configPath = config.DefaultPath(home)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	logger := app.NewLogger(stderr, cfg.Logging.Level)
	service, err := app.New(cfg, *configPath, logger)
	if err != nil {
		return err
	}

	addr := *listenAddr
	if addr == "" {
		addr = net.JoinHostPort(cfg.Server.BindHost, fmt.Sprintf("%d", cfg.Server.Port))
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	defer ln.Close()

	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return fmt.Errorf("listener address %T is not tcp", ln.Addr())
	}
	discovery, err := buildDiscoveryEvidence(cfg.Server.ServiceName, tcpAddr.Port)
	if err != nil {
		return fmt.Errorf("build discovery evidence: %w", err)
	}

	_, _ = fmt.Fprintf(stdout, "LISTEN http://%s\n", ln.Addr().String())
	logger.Info("control api listening", "host", tcpAddr.IP.String(), "port", tcpAddr.Port)
	logger.Info("discovery metadata ready",
		"service_type", discovery.Descriptor.ServiceType,
		"port", discovery.Port,
		"pairing", discovery.Descriptor.Pairing,
	)

	publisher := newDiscoveryPublisher()
	if publisher != nil {
		if err := publisher.Start(ctx, discovery.Descriptor, discovery.Port); err != nil {
			logger.Warn("bonjour advertisement unavailable", "error", err)
		} else {
			go func() {
				<-ctx.Done()
				_ = publisher.Stop(context.Background())
			}()
			defer func() {
				_ = publisher.Stop(context.Background())
			}()
		}
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- service.Serve(ctx, ln)
	}()

	select {
	case <-ctx.Done():
		return <-serveErr
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Stdout, os.Stderr, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "talka-server: %v\n", err)
		os.Exit(1)
	}
}
