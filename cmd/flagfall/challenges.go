package main

import (
	"archive/zip"
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/flagfall/flagfall/internal/api"
	"github.com/flagfall/flagfall/internal/challenges"
)

const challengesUsage = `usage:
  flagfall challenges check <folder | file.zip>
  flagfall challenges import [--yes] [--prune] <folder | file.zip>
  flagfall challenges export <empty folder>

check   validates challenge.yml files without touching the database (for CI).
import  adds new challenges and updates existing ones by slug.
        --yes    don't ask before importing (needed when not run in a terminal)
        --prune  also remove challenges that aren't in the folder: deleted if
                 nobody solved them, retired (points kept) if someone did
export  writes every challenge back out. Exact flags can't be recovered (they
        are stored as hashes) and are written as "` + challenges.RedactedFlag + `".
`

// Overridden in tests.
var (
	stdin       io.Reader = os.Stdin
	interactive           = func() bool {
		st, err := os.Stdin.Stat()
		return err == nil && st.Mode()&os.ModeCharDevice != 0
	}
)

func challengesCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, challengesUsage)
		return 2
	}
	sub, rest := args[0], args[1:]
	var target string
	var yes, prune bool
	for _, a := range rest {
		switch {
		case a == "--yes" && sub == "import":
			yes = true
		case a == "--prune" && sub == "import":
			prune = true
		case strings.HasPrefix(a, "-") || target != "":
			fmt.Fprint(stderr, challengesUsage)
			return 2
		default:
			target = a
		}
	}
	if target == "" {
		fmt.Fprint(stderr, challengesUsage)
		return 2
	}

	var err error
	switch sub {
	case "check":
		err = checkCmd(target, stdout)
	case "import":
		err = importCmd(target, yes, prune, stdout)
	case "export":
		err = exportCmd(target, stdout)
	default:
		fmt.Fprint(stderr, challengesUsage)
		return 2
	}
	var ps challenges.Problems
	switch {
	case errors.As(err, &ps):
		fmt.Fprintf(stderr, "%d problem(s):\n", len(ps))
		for _, p := range ps {
			fmt.Fprintln(stderr, "  "+p.String())
		}
		return 1
	case err != nil:
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// loadSpecs reads a folder (one challenge, or a folder of them) or a .zip.
func loadSpecs(target string) ([]*challenges.Spec, error) {
	st, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return challenges.LoadFolder(os.DirFS(target))
	}
	if !strings.HasSuffix(strings.ToLower(target), ".zip") {
		return nil, fmt.Errorf("%s: give a folder or a .zip", target)
	}
	zr, err := zip.OpenReader(target)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", target, err)
	}
	defer zr.Close()
	return challenges.LoadArchive(zr)
}

func checkCmd(target string, out io.Writer) error {
	specs, err := loadSpecs(target)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%d challenge(s) OK: %s\n", len(specs), slugs(specs))
	return nil
}

func importCmd(target string, yes, prune bool, out io.Writer) error {
	specs, err := loadSpecs(target)
	if err != nil {
		return err
	}
	ctx := context.Background()
	cfg, db, err := setup(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	plan, err := challenges.MakePlan(ctx, db, cfg.Secret(), specs)
	if err != nil {
		return err
	}
	ev, err := db.LoadEvent(ctx)
	if err != nil {
		return err
	}
	// An event with no start time counts as running for players, but here
	// it means one that hasn't been scheduled yet.
	running := ev.StartsAt != nil && ev.AcceptsSubmissions(time.Now())
	pruning := prune && len(plan.Missing) > 0

	fmt.Fprintf(out, "Importing %d challenge(s) from %s:\n%s", len(specs), target, plan.Summary(prune))
	if running {
		if pruning {
			return errors.New("the event is running, so --prune won't remove challenges; retire them in the admin panel instead")
		}
		var scored []string
		for _, c := range plan.Changed {
			if c.Scored() {
				scored = append(scored, c.Slug)
			}
		}
		fmt.Fprintln(out, "\nWarning: the event is running. Players see these changes at once.")
		if len(scored) > 0 {
			fmt.Fprintf(out, "Flags or points change for: %s. Teams that already solved them keep their solves.\n", strings.Join(scored, ", "))
		}
	}
	if len(plan.New) == 0 && len(plan.Changed) == 0 && !pruning {
		fmt.Fprintln(out, "\nNothing to do.")
		return nil
	}
	if !yes {
		if !interactive() {
			return errors.New("not running in a terminal; pass --yes to import without asking")
		}
		fmt.Fprint(out, "\nImport? [y/N] ")
		answer, _ := bufio.NewReader(stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			return errors.New("cancelled; nothing was changed")
		}
	}

	if _, err := challenges.Apply(ctx, db, cfg.Secret(), api.FilesDir(cfg.DataDir), specs); err != nil {
		return err
	}
	fmt.Fprintf(out, "Imported: %d new, %d updated.\n", len(plan.New), len(plan.Changed))
	if pruning {
		deleted, retired, err := challenges.Prune(ctx, db, plan.Missing)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Pruned: %d deleted, %d retired.\n", len(deleted), len(retired))
	}
	if running {
		fmt.Fprintln(out, "A running server shows the changes now; the scoreboard catches up within a minute.")
	}
	return nil
}

func exportCmd(target string, out io.Writer) error {
	ctx := context.Background()
	cfg, db, err := setup(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	res, err := challenges.Export(ctx, db, api.FilesDir(cfg.DataDir), target)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Exported %d challenge(s) and %d file(s) to %s.\n", res.Challenges, res.Files, target)
	if len(res.Redacted) > 0 {
		fmt.Fprintf(out, "\nSet the real flags before importing (exact flags are stored as hashes): %s\n", strings.Join(res.Redacted, ", "))
	}
	if len(res.Retired) > 0 {
		fmt.Fprintf(out, "Retired challenges were written as hidden: %s\n", strings.Join(res.Retired, ", "))
	}
	if res.TeamFlags > 0 {
		fmt.Fprintf(out, "%d per-team flag(s) were left out; the format has no place for them.\n", res.TeamFlags)
	}
	return nil
}

func slugs(specs []*challenges.Spec) string {
	s := make([]string, len(specs))
	for i, sp := range specs {
		s[i] = sp.Slug
	}
	return strings.Join(s, ", ")
}
