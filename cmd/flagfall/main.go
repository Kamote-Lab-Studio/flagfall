// Command flagfall runs the Flagfall CTF platform.
package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/flagfall/flagfall/internal/adminui"
	"github.com/flagfall/flagfall/internal/api"
	"github.com/flagfall/flagfall/internal/backup"
	"github.com/flagfall/flagfall/internal/blobs"
	"github.com/flagfall/flagfall/internal/config"
	"github.com/flagfall/flagfall/internal/live"
	"github.com/flagfall/flagfall/internal/seed"
	"github.com/flagfall/flagfall/internal/server"
	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/themes"
	"github.com/flagfall/flagfall/internal/themes/bundled"
	"github.com/flagfall/flagfall/internal/version"
	adminweb "github.com/flagfall/flagfall/web/admin"
)

const usage = `Flagfall: a self-hosted CTF platform.

Usage:
  flagfall <command>

Commands:
  serve        Start the server
  seed sample  Load the sample event (12 challenges, 312 teams) into an empty install
  backup <file>   Write a backup (database and uploaded files) to a .zip; "-" for stdout
  restore <file>  Restore a backup into an empty install (--replace to overwrite one; stop the server first)
  challenges check|import|export <folder>
               Validate, import or export challenge.yml folders (flagfall challenges for details)
  healthcheck  Exit 0 if the server on FLAGFALL_ADDR answers /healthz (for containers)
  version      Print the version and exit
  help         Show this help

Configuration comes from environment variables or flagfall.toml (see docs/dev/config.md):
  FLAGFALL_ADDR        address to listen on (default ":8080")
  FLAGFALL_PUBLIC_URL  public URL players use (default "http://localhost:8080")
  FLAGFALL_DATA_DIR    data directory (default "./data")
  FLAGFALL_DATABASE_URL  database URL (default: SQLite in the data directory)
  FLAGFALL_SEED_SAMPLE   "true" loads the sample event and demo logins on the first serve (demos only)
  FLAGFALL_S3_BUCKET     store challenge files in an S3-compatible bucket (see docs/dev/config.md)
`

func backupCmd(file string, stdout, stderr io.Writer) error {
	ctx := context.Background()
	_, db, files, err := setup(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	var w io.Writer = stdout
	var f *os.File
	if file != "-" {
		// Write next to the target and rename, so a failed run leaves no half file.
		f, err = os.CreateTemp(filepath.Dir(file), ".flagfall-backup-*")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		defer f.Close()
		w = f
	}
	m, err := backup.Write(ctx, db, files, version.Version, time.Now(), w)
	if err != nil {
		return err
	}
	if f != nil {
		if err := f.Close(); err != nil {
			return err
		}
		if err := os.Rename(f.Name(), file); err != nil {
			return err
		}
	}
	total := 0
	for _, n := range m.Rows {
		total += n
	}
	fmt.Fprintf(stderr, "Backed up %d rows and %d files", total, m.Files)
	if file != "-" {
		fmt.Fprintf(stderr, " to %s", file)
	}
	fmt.Fprintln(stderr, ".")
	if len(m.MissingFiles) > 0 {
		fmt.Fprintf(stderr, "Warning: %d challenge files were missing from storage and aren't in the backup.\n", len(m.MissingFiles))
	}
	return nil
}

func restoreCmd(file string, replace bool, out io.Writer) error {
	ctx := context.Background()
	_, db, files, err := setup(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	if !replace {
		for _, m := range []any{(*store.User)(nil), (*store.Challenge)(nil)} {
			if n, err := db.DB.NewSelect().Model(m).Count(ctx); err != nil {
				return err
			} else if n > 0 {
				return errors.New("this install isn't empty; pass --replace to overwrite everything in it")
			}
		}
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(f, st.Size())
	if err != nil {
		return fmt.Errorf("%s isn't a Flagfall backup (.zip): %w", file, err)
	}
	m, err := backup.Restore(ctx, db, files, zr)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Restored the backup from %s (%d teams, %d users, %d files). Everyone will need to sign in again.\n",
		m.CreatedAt.Format(time.RFC1123), m.Rows["teams"], m.Rows["users"], m.Files)
	return nil
}

func truthy(v string) bool { return v == "1" || strings.EqualFold(v, "true") }

// healthcheck asks the local server for /healthz. Container images have no
// curl, so the binary checks itself.
func healthcheck() error {
	addr := os.Getenv("FLAGFALL_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("FLAGFALL_ADDR %q: %w", addr, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("/healthz answered %d", res.StatusCode)
	}
	return nil
}

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
	case "backup":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: flagfall backup <file.zip | ->")
			return 2
		}
		if err := backupCmd(args[1], stdout, stderr); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	case "restore":
		file, replace := "", false
		for _, a := range args[1:] {
			if a == "--replace" {
				replace = true
			} else if file == "" {
				file = a
			} else {
				file = ""
				break
			}
		}
		if file == "" {
			fmt.Fprintln(stderr, "usage: flagfall restore [--replace] <file.zip>")
			return 2
		}
		if err := restoreCmd(file, replace, stdout); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	case "challenges":
		return challengesCmd(args[1:], stdout, stderr)
	case "healthcheck":
		if err := healthcheck(); err != nil {
			fmt.Fprintln(stderr, "unhealthy:", err)
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

	cfg, db, files, err := setup(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	// Demo containers load the sample on first start; later starts find it
	// already there and skip.
	if truthy(os.Getenv("FLAGFALL_SEED_SAMPLE")) {
		if err := loadSample(ctx, cfg, db, files, logOut); errors.Is(err, seed.ErrNotEmpty) {
			logger.Info("FLAGFALL_SEED_SAMPLE is set, but the install isn't empty; not loading the sample")
		} else if err != nil {
			return fmt.Errorf("load the sample event: %w", err)
		}
	}

	hub := live.NewHub()
	defer hub.Close()
	installed, err := bundled.Load()
	if err != nil {
		return err
	}
	themeManager := themes.NewManager(themes.NewRegistry(installed...), db, logger)
	playerAPI := api.New(api.Options{Store: db, Config: cfg, Hub: hub, Files: files, Logger: logger,
		Themes: themeManager})
	themeManager.PreviewTheme = playerAPI.PreviewTheme
	if code, required, err := playerAPI.PrepareSetup(ctx); err != nil {
		return fmt.Errorf("check for an owner account: %w", err)
	} else if required {
		logger.Warn("no owner account yet: open the admin panel to finish setup",
			"url", cfg.PublicURL+"/admin", "setup_code", code)
	}

	for _, t := range installed {
		if err := t.Register(ctx, db, time.Now()); err != nil {
			return fmt.Errorf("register the %s theme: %w", t.Manifest.ID, err)
		}
		if !t.Built() {
			logger.Warn("a bundled theme isn't built; it can't be previewed or published (run make web-build, then rebuild)", "theme", t.Manifest.ID)
		}
	}
	themeHandler := themes.New(themeManager)
	adminPanel := adminui.New(adminweb.FS).WithHead(themeHandler.AdminHead)
	if !adminPanel.Built() {
		logger.Warn("the admin panel isn't built; /admin shows a build reminder (run make web-build, then rebuild)")
	}

	// Pushes scheduled announcements to players when they go out, and makes
	// scheduled theme switches live.
	go playerAPI.RunScheduler(ctx, 15*time.Second)

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: server.New(server.Options{
			Logger:     logger,
			TrustProxy: cfg.TrustProxy,
			Mount:      []func(chi.Router){playerAPI.Mount, playerAPI.MountAdmin, adminPanel.Mount, themeHandler.Mount},
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

// setup loads the config, the secret key, a migrated database and the
// file store (checked, so a wrong bucket fails here, not on the first
// download).
func setup(ctx context.Context) (config.Config, *store.Store, *blobs.Store, error) {
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		return cfg, nil, nil, err
	}
	if err := cfg.EnsureSecret(); err != nil {
		return cfg, nil, nil, err
	}
	files, err := blobs.FromConfig(cfg)
	if err != nil {
		return cfg, nil, nil, err
	}
	if err := files.Check(ctx); err != nil {
		return cfg, nil, nil, fmt.Errorf("file storage (%s): %w", files.Location, err)
	}
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return cfg, nil, nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		return cfg, nil, nil, err
	}
	if err := db.EnsureEvent(ctx); err != nil {
		db.Close()
		return cfg, nil, nil, err
	}
	return cfg, db, files, nil
}

func seedSample(out io.Writer) error {
	ctx := context.Background()
	cfg, db, files, err := setup(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	return loadSample(ctx, cfg, db, files, out)
}

// loadSample loads the sample event and its demo logins. It returns
// seed.ErrNotEmpty if the install already has challenges or teams.
func loadSample(ctx context.Context, cfg config.Config, db *store.Store, files *blobs.Store, out io.Writer) error {
	sum, err := seed.Sample(ctx, db, cfg.Secret(), files, time.Now())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Loaded the sample event: %d challenges, %d teams, %d players, %d solves.\n\n", sum.Challenges, sum.Teams, sum.Players, sum.Solves)
	if err := seed.DemoLogins(ctx, db, time.Now()); errors.Is(err, seed.ErrHasOwner) {
		fmt.Fprintln(out, "This install already has an owner, so no public demo logins were added.")
		return nil
	} else if err != nil {
		return err
	}
	fmt.Fprintf(out, "Demo logins (shown on the sign-in pages; anyone can use them):\n")
	fmt.Fprintf(out, "  player (captain of null_pointer): %s\n", seed.DemoEmail)
	fmt.Fprintf(out, "  owner (admin panel at /admin):    %s\n", seed.DemoOwnerEmail)
	fmt.Fprintf(out, "  password for both:                %s\n", seed.DemoPassword)
	return nil
}
