// Package challenges reads challenge.yml folders and writes them to the
// database. See docs/dev/challenge-format.md.
//
// Phase 1 uses it to load the sample pack. Phase 2 builds the
// `flagfall challenges check/import/export` commands and the admin upload on
// the same Load and Apply functions.
package challenges

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/uptrace/bun"
	"gopkg.in/yaml.v3"

	"github.com/Kamote-Lab-Studio/flagfall/internal/blobs"
	"github.com/Kamote-Lab-Studio/flagfall/internal/flags"
	"github.com/Kamote-Lab-Studio/flagfall/internal/store"
)

// Spec is a parsed challenge.yml.
type Spec struct {
	Format      int            `yaml:"format"`
	Slug        string         `yaml:"slug"`
	Name        string         `yaml:"name"`
	Category    string         `yaml:"category"`
	Difficulty  string         `yaml:"difficulty"`
	Author      string         `yaml:"author"`
	Description string         `yaml:"description"`
	Value       *int           `yaml:"value"`
	Scoring     *DynamicSpec   `yaml:"scoring"`
	Flags       []FlagSpec     `yaml:"flags"`
	Hints       []HintSpec     `yaml:"hints"`
	Files       []string       `yaml:"files"`
	Connection  string         `yaml:"connection"`
	Service     *ServiceYAML   `yaml:"service"`
	ThemeFields map[string]any `yaml:"theme_fields"`
	Tags        []string       `yaml:"tags"`
	VisibleFrom *time.Time     `yaml:"visible_from"`
	State       string         `yaml:"state"`
	Requires    []string       `yaml:"requires"`

	// Filled in by Load.
	dir           string
	descriptionMD string
	hintContent   []string
	files         []loadedFile
	service       *ServiceSpec
	buildContext  []byte // tar of the folder, when the service is built from a Dockerfile
}

type DynamicSpec struct {
	Initial int `yaml:"initial"`
	Minimum int `yaml:"minimum"`
	Decay   int `yaml:"decay"`
}

type FlagSpec struct {
	Type  string `yaml:"type"`
	Value string `yaml:"value"`
}

type HintSpec struct {
	Cost    int    `yaml:"cost"`
	Content string `yaml:"content"`
}

type loadedFile struct {
	name   string
	data   []byte
	sha256 string
}

// Problem is one validation error.
type Problem struct {
	Path    string // e.g. cookie-jar/challenge.yml
	Message string
}

func (p Problem) String() string { return p.Path + ": " + p.Message }

// Problems is a list of validation errors; it implements error.
type Problems []Problem

func (ps Problems) Error() string {
	lines := make([]string, len(ps))
	for i, p := range ps {
		lines[i] = p.String()
	}
	return strings.Join(lines, "\n")
}

var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// MaxFileSize is the default limit for a challenge download.
const MaxFileSize = 100 << 20

// Load reads every */challenge.yml under root in fsys and validates it.
// It returns all problems found, not just the first.
func Load(fsys fs.FS, root string) ([]*Spec, error) {
	matches, err := fs.Glob(fsys, path.Join(root, "*", "challenge.yml"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no */challenge.yml files found in %s", root)
	}
	sort.Strings(matches)

	var specs []*Spec
	var problems Problems
	seen := map[string]string{}
	for _, m := range matches {
		s, ps := loadOne(fsys, m)
		problems = append(problems, ps...)
		if s == nil {
			continue
		}
		if prev, dup := seen[s.Slug]; dup {
			problems = append(problems, Problem{m, fmt.Sprintf("slug %q is also used by %s", s.Slug, prev)})
			continue
		}
		seen[s.Slug] = m
		specs = append(specs, s)
	}
	if len(problems) > 0 {
		return nil, problems
	}
	return specs, nil
}

func loadOne(fsys fs.FS, file string) (*Spec, Problems) {
	var ps Problems
	bad := func(format string, args ...any) { ps = append(ps, Problem{file, fmt.Sprintf(format, args...)}) }

	raw, err := fs.ReadFile(fsys, file)
	if err != nil {
		bad("can't read: %v", err)
		return nil, ps
	}
	s := new(Spec)
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(s); err != nil {
		bad("invalid YAML: %v", err)
		return nil, ps
	}
	s.dir = path.Dir(file)

	if s.Format != 0 && s.Format != 1 {
		bad("format %d is not supported (this version reads format 1)", s.Format)
	}
	if !slugRE.MatchString(s.Slug) {
		bad("slug %q must be lowercase letters, digits and dashes (max 64)", s.Slug)
	}
	if strings.TrimSpace(s.Name) == "" || len(s.Name) > 80 {
		bad("name is required (max 80 characters)")
	}
	if strings.TrimSpace(s.Category) == "" || len(s.Category) > 32 {
		bad("category is required (max 32 characters)")
	}
	switch strings.ToLower(s.Difficulty) {
	case "", "easy", "medium", "hard", "insane":
	default:
		bad("difficulty must be Easy, Medium, Hard or Insane")
	}
	switch s.State {
	case "", "visible", "hidden":
	default:
		bad("state must be visible or hidden")
	}

	switch {
	case s.Value != nil && s.Scoring != nil:
		bad("use either value or scoring, not both")
	case s.Value == nil && s.Scoring == nil:
		bad("value (or scoring) is required")
	case s.Value != nil && *s.Value < 0:
		bad("value can't be negative")
	case s.Scoring != nil:
		d := s.Scoring
		if d.Initial < 1 || d.Minimum < 0 || d.Minimum > d.Initial || d.Decay < 1 {
			bad("scoring needs initial >= 1, 0 <= minimum <= initial, and decay >= 1")
		}
	}

	if len(s.Flags) == 0 {
		bad("at least one flag is required")
	}
	for i, f := range s.Flags {
		switch {
		case f.Value == "" || len(f.Value) > flags.MaxLength:
			bad("flags[%d]: value is required (max %d bytes)", i, flags.MaxLength)
		case strings.HasPrefix(f.Value, "<redacted"):
			bad("flags[%d]: replace the redacted placeholder with the real flag", i)
		}
		switch f.Type {
		case flags.Exact, flags.CaseInsensitive:
		case flags.Pattern:
			if _, err := flags.CompilePattern(f.Value); err != nil {
				bad("flags[%d]: %v", i, err)
			}
		default:
			bad("flags[%d]: type must be exact, case-insensitive or pattern", i)
		}
	}

	if s.Description == "" {
		bad("description is required")
	} else {
		var missing bool
		if s.descriptionMD, missing = readTextOrInline(fsys, s.dir, s.Description); missing {
			bad("description: %q isn't in the challenge folder", s.Description)
		}
	}
	for _, m := range placeholderRE.FindAllStringSubmatch(s.Connection, -1) {
		if !Placeholders[m[1]] {
			bad("connection: unknown placeholder {{%s}} (known: {{event_domain}})", m[1])
		}
	}
	for i, h := range s.Hints {
		if h.Cost < 0 {
			bad("hints[%d]: cost can't be negative", i)
		}
		if h.Content == "" {
			bad("hints[%d]: content is required", i)
		}
		text, missing := readTextOrInline(fsys, s.dir, h.Content)
		if missing {
			bad("hints[%d]: %q isn't in the challenge folder", i, h.Content)
		}
		s.hintContent = append(s.hintContent, text)
	}
	for _, f := range s.Files {
		clean := path.Clean(f)
		if path.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			bad("files: %q must be inside the challenge folder", f)
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join(s.dir, clean))
		if err != nil {
			bad("files: can't read %q", f)
			continue
		}
		if len(data) > MaxFileSize {
			bad("files: %q is larger than %d MB", f, MaxFileSize>>20)
			continue
		}
		sum := sha256.Sum256(data)
		s.files = append(s.files, loadedFile{name: path.Base(clean), data: data, sha256: hex.EncodeToString(sum[:])})
	}
	if s.Service != nil {
		s.service, s.buildContext = checkService(fsys, s.dir, s.Service, bad)
	}
	if len(ps) > 0 {
		return nil, ps
	}
	return s, nil
}

// readTextOrInline returns the file's contents if value names a file in dir,
// and value itself otherwise. missing is true when value looks like a file
// name (one word ending in .md or .txt) but there's no such file, which
// would otherwise show players the file name as the text.
func readTextOrInline(fsys fs.FS, dir, value string) (text string, missing bool) {
	if fileLike.MatchString(value) {
		if b, err := fs.ReadFile(fsys, path.Join(dir, path.Clean(value))); err == nil {
			return string(b), false
		}
		return value, true
	}
	return value, false
}

var fileLike = regexp.MustCompile(`^\S+\.(md|txt)$`)

// placeholderRE finds {{name}} in connection strings.
var placeholderRE = regexp.MustCompile(`\{\{([^}]*)\}\}`)

// Placeholders are the {{names}} a connection string may use; the player
// API fills them in.
var Placeholders = map[string]bool{"event_domain": true}

// Result summarizes an Apply.
type Result struct {
	Created, Updated []string
}

// Apply creates or updates the challenges by slug, in one transaction.
// Files are stored in files by SHA-256. Flags and hints are replaced;
// per-team flags, which the format can't express, are kept.
func Apply(ctx context.Context, db *store.Store, secret []byte, files *blobs.Store, specs []*Spec) (Result, error) {
	var res Result
	for _, s := range specs {
		for _, f := range s.files {
			if err := files.Put(ctx, f.sha256, bytes.NewReader(f.data), int64(len(f.data))); err != nil {
				return res, err
			}
		}
		if s.buildContext != nil {
			if err := files.Put(ctx, s.service.Context, bytes.NewReader(s.buildContext), int64(len(s.buildContext))); err != nil {
				return res, err
			}
		}
	}

	now := time.Now().UTC()
	err := db.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		for i, s := range specs {
			c := new(store.Challenge)
			err := tx.NewSelect().Model(c).Where("slug = ?", s.Slug).Scan(ctx)
			exists := err == nil
			if err != nil && !isNoRows(err) {
				return err
			}

			s.fill(c)
			if !exists {
				c.Position, c.CreatedAt = i, now
			}
			c.UpdatedAt = now

			if exists {
				if _, err := tx.NewUpdate().Model(c).WherePK().ExcludeColumn("id", "created_at", "position").Exec(ctx); err != nil {
					return err
				}
				// Per-team flags aren't in the format, so a re-import keeps them.
				if _, err := tx.NewDelete().Model((*store.Flag)(nil)).Where("challenge_id = ? AND team_id IS NULL", c.ID).Exec(ctx); err != nil {
					return err
				}
				if _, err := tx.NewDelete().Model((*store.File)(nil)).Where("challenge_id = ?", c.ID).Exec(ctx); err != nil {
					return err
				}
				// Hints are replaced only when they changed, so unlocks survive re-imports.
				res.Updated = append(res.Updated, s.Slug)
			} else {
				if _, err := tx.NewInsert().Model(c).Exec(ctx); err != nil {
					return err
				}
				res.Created = append(res.Created, s.Slug)
			}

			for _, f := range s.Flags {
				fl := &store.Flag{ChallengeID: c.ID, Kind: f.Type}
				if f.Type == flags.Pattern {
					fl.Pattern = strPtr(f.Value)
				} else {
					h := flags.Hash(secret, f.Type, f.Value)
					fl.ValueHash = &h
				}
				if _, err := tx.NewInsert().Model(fl).Exec(ctx); err != nil {
					return err
				}
			}
			if err := syncHints(ctx, tx, c.ID, s); err != nil {
				return err
			}
			for _, f := range s.files {
				if _, err := tx.NewInsert().Model(&store.File{
					ChallengeID: c.ID, Name: f.name, StorageKey: f.sha256, Size: int64(len(f.data)), SHA256: f.sha256,
				}).Exec(ctx); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return res, err
}

// fill sets the challenge columns the spec controls.
func (s *Spec) fill(c *store.Challenge) {
	themeFields, _ := json.Marshal(orEmpty(s.ThemeFields))
	tags, _ := json.Marshal(orEmptyList(s.Tags))
	requires, _ := json.Marshal(orEmptyList(s.Requires))
	c.Slug, c.Name, c.Category = s.Slug, s.Name, s.Category
	c.Difficulty = strPtr(strings.ToLower(s.Difficulty))
	c.Author, c.DescriptionMD = s.Author, s.descriptionMD
	c.Connection = strPtr(s.Connection)
	c.ThemeFields, c.Tags, c.Requires = string(themeFields), string(tags), string(requires)
	c.VisibleFrom = s.VisibleFrom
	c.Service = nil
	if s.service != nil {
		b, _ := json.Marshal(s.service)
		c.Service = strPtr(string(b))
	}
	c.State = s.State
	if c.State == "" {
		c.State = "visible"
	}
	if s.Scoring != nil {
		c.Value, c.MinValue, c.Decay = s.Scoring.Initial, &s.Scoring.Minimum, &s.Scoring.Decay
	} else {
		c.Value, c.MinValue, c.Decay = *s.Value, nil, nil
	}
}

func syncHints(ctx context.Context, tx bun.Tx, challengeID int64, s *Spec) error {
	var current []store.Hint
	if err := tx.NewSelect().Model(&current).Where("challenge_id = ?", challengeID).Order("position", "id").Scan(ctx); err != nil {
		return err
	}
	same := len(current) == len(s.Hints)
	for i := 0; same && i < len(current); i++ {
		same = current[i].Cost == s.Hints[i].Cost && current[i].ContentMD == s.hintContent[i]
	}
	if same {
		return nil
	}
	if _, err := tx.NewDelete().Model((*store.Hint)(nil)).Where("challenge_id = ?", challengeID).Exec(ctx); err != nil {
		return err
	}
	for i, h := range s.Hints {
		if _, err := tx.NewInsert().Model(&store.Hint{ChallengeID: challengeID, Cost: h.Cost, ContentMD: s.hintContent[i], Position: i}).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func orEmptyList(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

// ValidSlug reports whether s can be a challenge slug.
func ValidSlug(s string) bool { return slugRE.MatchString(s) }

// LoadArchive reads challenge folders from a zip. The folders can be at the
// top of the archive or inside a single top-level folder.
func LoadArchive(zr fs.FS) ([]*Spec, error) {
	if m, _ := fs.Glob(zr, "*/challenge.yml"); len(m) > 0 {
		return Load(zr, ".")
	}
	entries, err := fs.ReadDir(zr, ".")
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), "__MACOSX") {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 1 {
		return Load(zr, dirs[0])
	}
	return nil, fmt.Errorf("no */challenge.yml files found in the archive")
}
