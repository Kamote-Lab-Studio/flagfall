package challenges

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/flagfall/flagfall/internal/blobs"
	"github.com/flagfall/flagfall/internal/flags"
	"github.com/flagfall/flagfall/internal/store"
)

// RedactedFlag replaces exact flags on export: they're stored as hashes and
// can't be recovered. Load refuses it, so nobody imports it by mistake.
const RedactedFlag = "<redacted: set before import>"

// ExportResult says what Export wrote and what needs a person's attention.
type ExportResult struct {
	Challenges int
	Files      int
	// Redacted lists the challenges whose exact flags must be filled in.
	Redacted []string
	// Retired lists retired challenges, written as hidden.
	Retired []string
	// TeamFlags counts per-team flags, which the format can't express and
	// were left out.
	TeamFlags int
}

// exportSpec is Spec with omitempty, so exported files stay short.
type exportSpec struct {
	Format      int            `yaml:"format"`
	Slug        string         `yaml:"slug"`
	Name        string         `yaml:"name"`
	Category    string         `yaml:"category"`
	Difficulty  string         `yaml:"difficulty,omitempty"`
	Author      string         `yaml:"author,omitempty"`
	Description string         `yaml:"description"`
	Value       *int           `yaml:"value,omitempty"`
	Scoring     *DynamicSpec   `yaml:"scoring,omitempty"`
	Flags       []FlagSpec     `yaml:"flags"`
	Hints       []HintSpec     `yaml:"hints,omitempty"`
	Files       []string       `yaml:"files,omitempty"`
	Connection  string         `yaml:"connection,omitempty"`
	ThemeFields map[string]any `yaml:"theme_fields,omitempty"`
	Tags        []string       `yaml:"tags,omitempty"`
	VisibleFrom *time.Time     `yaml:"visible_from,omitempty"`
	State       string         `yaml:"state,omitempty"`
	Requires    []string       `yaml:"requires,omitempty"`
}

// Export writes every challenge to outDir/<slug>/ in the challenge.yml
// format: challenge.yml, description.md, hints/N.md and dist/<files>.
// outDir must be empty or not exist.
func Export(ctx context.Context, db *store.Store, files *blobs.Store, outDir string) (ExportResult, error) {
	var res ExportResult
	if entries, err := os.ReadDir(outDir); err == nil && len(entries) > 0 {
		return res, fmt.Errorf("%s isn't empty; export into a new folder", outDir)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return res, err
	}

	var cs []store.Challenge
	if err := db.DB.NewSelect().Model(&cs).Order("position", "id").Scan(ctx); err != nil {
		return res, err
	}
	for i := range cs {
		c := &cs[i]
		if !ValidSlug(c.Slug) {
			return res, fmt.Errorf("challenge %d has slug %q, which can't be a folder name", c.ID, c.Slug)
		}
		dir := filepath.Join(outDir, c.Slug)
		spec := exportSpec{
			Format: 1, Slug: c.Slug, Name: c.Name, Category: c.Category,
			Difficulty: title(deref(c.Difficulty)), Author: c.Author,
			Description: "description.md", Connection: deref(c.Connection), VisibleFrom: c.VisibleFrom,
		}
		if c.MinValue != nil && c.Decay != nil {
			spec.Scoring = &DynamicSpec{Initial: c.Value, Minimum: *c.MinValue, Decay: *c.Decay}
		} else {
			v := c.Value
			spec.Value = &v
		}
		switch c.State {
		case "hidden":
			spec.State = "hidden"
		case "retired":
			spec.State = "hidden"
			res.Retired = append(res.Retired, c.Slug)
		}
		_ = json.Unmarshal([]byte(c.ThemeFields), &spec.ThemeFields)
		_ = json.Unmarshal([]byte(c.Tags), &spec.Tags)
		_ = json.Unmarshal([]byte(c.Requires), &spec.Requires)
		if err := writeText(filepath.Join(dir, "description.md"), c.DescriptionMD); err != nil {
			return res, err
		}

		var fl []store.Flag
		if err := db.DB.NewSelect().Model(&fl).Where("challenge_id = ?", c.ID).Order("id").Scan(ctx); err != nil {
			return res, err
		}
		redacted := false
		for _, f := range fl {
			switch {
			case f.TeamID != nil:
				res.TeamFlags++
			case f.Kind == flags.Pattern:
				spec.Flags = append(spec.Flags, FlagSpec{Type: f.Kind, Value: deref(f.Pattern)})
			default:
				spec.Flags = append(spec.Flags, FlagSpec{Type: f.Kind, Value: RedactedFlag})
				redacted = true
			}
		}
		if redacted {
			res.Redacted = append(res.Redacted, c.Slug)
		}

		var hints []store.Hint
		if err := db.DB.NewSelect().Model(&hints).Where("challenge_id = ?", c.ID).Order("position", "id").Scan(ctx); err != nil {
			return res, err
		}
		for n, h := range hints {
			name := fmt.Sprintf("hints/%d.md", n+1)
			if err := writeText(filepath.Join(dir, filepath.FromSlash(name)), h.ContentMD); err != nil {
				return res, err
			}
			spec.Hints = append(spec.Hints, HintSpec{Cost: h.Cost, Content: name})
		}

		var stored []store.File
		if err := db.DB.NewSelect().Model(&stored).Where("challenge_id = ?", c.ID).Order("id").Scan(ctx); err != nil {
			return res, err
		}
		used := map[string]bool{}
		for _, f := range stored {
			name := safeFileName(f.Name, used)
			if err := copyBlob(ctx, files, f.StorageKey, filepath.Join(dir, "dist", name)); err != nil {
				return res, fmt.Errorf("%s: file %s: %w", c.Slug, f.Name, err)
			}
			spec.Files = append(spec.Files, "dist/"+name)
			res.Files++
		}

		b, err := yaml.Marshal(spec)
		if err != nil {
			return res, err
		}
		if err := writeText(filepath.Join(dir, "challenge.yml"), string(b)); err != nil {
			return res, err
		}
		res.Challenges++
	}
	return res, nil
}

func writeText(file, text string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, []byte(text), 0o644)
}

func copyBlob(ctx context.Context, files *blobs.Store, key, dst string) error {
	in, err := files.Open(ctx, key)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// safeFileName keeps a stored download name usable as a file name, and
// unique within the challenge.
func safeFileName(name string, used map[string]bool) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == ".." || name == "" {
		name = "file"
	}
	base, n := name, 2
	for used[strings.ToLower(name)] {
		ext := path.Ext(base)
		name = fmt.Sprintf("%s-%d%s", strings.TrimSuffix(base, ext), n, ext)
		n++
	}
	used[strings.ToLower(name)] = true
	return name
}

func title(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
