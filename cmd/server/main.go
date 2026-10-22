// @title Resilient Catalog Synchronization Engine API
// @version 1.0
// @description High-performance, fault-tolerant catalog synchronization engine with SQLite multi-tenancy, real-time Server-Sent Events (SSE), and dead-letter quarantine resolution.
// @termsOfService http://swagger.io/terms/

// @contact.name Coderpush Engineering Team
// @contact.url https://coderpush.com
// @contact.email dev@coderpush.com

// @license.name MIT
// @license.url https://opensource.org/licenses/MIT

// @host localhost:8080
// @BasePath /
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"coderpush/pkg/api"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// ServerOptions contains parsed CLI flags.
type ServerOptions struct {
	Port      int
	DBPath    string
	StaticDir string
}

// ParseFlags parses command-line arguments for the server.
func ParseFlags(args []string, stderr io.Writer) (*ServerOptions, error) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.SetOutput(stderr)

	defaultPort := 8080
	if envPort := os.Getenv("PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil && p > 0 {
			defaultPort = p
		}
	}

	defaultDB := "catalog.db"
	if envDB := os.Getenv("DB_PATH"); envDB != "" {
		defaultDB = envDB
	}

	opts := &ServerOptions{}
	fs.IntVar(&opts.Port, "port", defaultPort, "HTTP server listen port (default 8080 or $PORT)")
	fs.StringVar(&opts.DBPath, "db", defaultDB, "SQLite database file path (or :memory: or $DB_PATH)")
	fs.StringVar(&opts.StaticDir, "static-dir", "", "Optional disk directory for static assets (overrides embedded files)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if opts.Port <= 0 || opts.Port > 65535 {
		return nil, fmt.Errorf("invalid --port: %d (must be 1-65535)", opts.Port)
	}
	return opts, nil
}

// Run executes the server using the provided context and options.
func Run(ctx context.Context, opts *ServerOptions, stdout, stderr io.Writer) error {
	if opts.DBPath != ":memory:" {
		if dir := filepath.Dir(opts.DBPath); dir != "." && dir != "" {
			_ = os.MkdirAll(dir, 0755)
		}
	}

	store, err := storage.Open(opts.DBPath)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}
	defer store.Close()

	// Startup Crash Recovery
	recovered, err := store.RecoverInterruptedJobs(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "Startup recovery error: %v\n", err)
	} else if len(recovered) > 0 {
		fmt.Fprintf(stdout, "[server] startup recovery marked %d interrupted jobs: %v\n", len(recovered), recovered)
	}

	// Initialize SSE Broker
	broker := api.NewBroker()
	go broker.Start(ctx)
	defer broker.Close()

	// Initialize Sync Engine with SSE Progress Hook
	progressHook := api.NewProgressHook(broker)
	engine := catalogSync.NewEngine(store, catalogSync.WithProgressHook(progressHook))

	// Construct HTTP handler stack with multi-tenant data directory
	dataDir := "data"
	if envData := os.Getenv("DATA_DIR"); envData != "" {
		dataDir = envData
	} else if opts.DBPath != ":memory:" && opts.DBPath != "" {
		dataDir = filepath.Dir(opts.DBPath)
	}
	serverHandler := api.NewServer(store, engine, broker, opts.StaticDir, api.WithDataDir(dataDir))

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", opts.Port),
		Handler:           serverHandler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      0, // Required for persistent SSE connections
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		fmt.Fprintf(stdout, "[server] listening on http://localhost:%d\n", opts.Port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("HTTP server error: %w", err)
	case <-ctx.Done():
		fmt.Fprintf(stdout, "\n[server] shutting down gracefully...\n")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	return nil
}

// runMainWithContext executes the server with an explicitly supplied context.
func runMainWithContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	opts, err := ParseFlags(args, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "Error parsing flags: %v\n", err)
		return 2
	}

	if err := Run(ctx, opts, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "Runtime error: %v\n", err)
		return 1
	}

	return 0
}

// runMain executes the server with process signal interception.
func runMain(args []string, stdout, stderr io.Writer) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return runMainWithContext(ctx, args, stdout, stderr)
}

var exitFunc = os.Exit

func main() {
	exitFunc(runMain(os.Args[1:], os.Stdout, os.Stderr))
}
