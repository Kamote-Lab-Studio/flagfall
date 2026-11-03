// Command flagfall runs the Flagfall CTF platform.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/flagfall/flagfall/internal/config"
	"github.com/flagfall/flagfall/internal/server"
	"github.com/flagfall/flagfall/internal/version"
)

const usage = `Flagfall: a self-hosted CTF platform.

Usage:
  flagfall <command>

Commands:
  serve     Start the server
  version   Print the version and exit
  help      Show this help

Configuration comes from environment variables (see docs/dev/config.md):
  FLAGFALL_ADDR        address to listen on (default ":8080")
  FLAGFALL_PUBLIC_URL  public URL players use (default "http://localhost:8080")
  FLAGFALL_DATA_DIR    data directory (default "./data")
  FLAGFALL_DATABASE_URL  database URL (default: SQLite in the data directory)
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "serve":
		if err := serve(stderr); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, version.String())
		return 0
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func serve(logOut io.Writer) error {
	logger := slog.New(slog.NewTextHandler(logOut, nil))

	cfg, err := config.FromEnv(os.LookupEnv)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.New(server.Options{Logger: logger}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("flagfall listening", "addr", cfg.Addr, "public_url", cfg.PublicURL, "version", version.Version)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
