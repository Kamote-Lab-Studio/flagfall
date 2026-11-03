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

	"github.com/go-chi/chi/v5"

	"github.com/flagfall/flagfall/internal/api"
	"github.com/flagfall/flagfall/internal/config"
	"github.com/flagfall/flagfall/internal/live"
	"github.com/flagfall/flagfall/internal/seed"
	"github.com/flagfall/flagfall/internal/server"
	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/themes"
	"github.com/flagfall/flagfall/internal/version"
	amberphosphor "github.com/flagfall/flagfall/themes/amber-phosphor"
)

const usage = `Flagfall: a self-hosted CTF platform.

Usage:
  flagfall <command>

Commands:
  serve        Start the server
  seed sample  Load the sample event (12 challenges, 312 teams) into an empty install
  version      Print the version and exit
  help         Show this help

Configuration comes from environment variables or flagfall.toml (see docs/dev/config.md):
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
	case "seed":
		if len(args) != 2 || args[1] != "sample" {
			fmt.Fprintln(stderr, "usage: flagfall seed sample")
			return 2
		}
		if err := seedSample(stdout); err != nil {
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, db, err := setup(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	hub := live.NewHub()
	defer hub.Close()
	playerAPI := api.New(api.Options{Store: db, Config: cfg, Hub: hub, Logger: logger})

	theme, err := themes.Load(amberphosphor.FS)
	if err != nil {
		return fmt.Errorf("load the default theme: %w", err)
	}
	if err := theme.Register(ctx, db, time.Now()); err != nil {
		return fmt.Errorf("register the default theme: %w", err)
	}
	if !theme.Built() {
		logger.Warn("the player theme isn't built; pages show a build reminder (run make web-build, then rebuild)")
	}
	themeHandler := themes.New(theme, db, logger)

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: server.New(server.Options{
			Logger:     logger,
			TrustProxy: cfg.TrustProxy,
			Mount:      []func(chi.Router){playerAPI.Mount, themeHandler.Mount},
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

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
		hub.Close() // end live streams so Shutdown doesn't wait on them
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// setup loads the config, the secret key and a migrated database.
func setup(ctx context.Context) (config.Config, *store.Store, error) {
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		return cfg, nil, err
	}
	if err := cfg.EnsureSecret(); err != nil {
		return cfg, nil, err
	}
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return cfg, nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		return cfg, nil, err
	}
	if err := db.EnsureEvent(ctx); err != nil {
		db.Close()
		return cfg, nil, err
	}
	return cfg, db, nil
}

func seedSample(out io.Writer) error {
	ctx := context.Background()
	cfg, db, err := setup(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	sum, err := seed.Sample(ctx, db, cfg.Secret(), api.FilesDir(cfg.DataDir), time.Now())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Loaded the sample event: %d challenges, %d teams, %d players, %d solves.\n\n", sum.Challenges, sum.Teams, sum.Players, sum.Solves)
	fmt.Fprintf(out, "Sign in as the captain of null_pointer to try it:\n  email:    %s\n  password: %s\n", seed.DemoEmail, seed.DemoPassword)
	return nil
}
