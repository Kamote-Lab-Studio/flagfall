package challenges

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/flagfall/flagfall/internal/flags"
	"github.com/flagfall/flagfall/internal/store"
)

// LoadFolder reads either one challenge folder (with challenge.yml at its
// top) or a folder of challenge folders.
func LoadFolder(fsys fs.FS) ([]*Spec, error) {
	if _, err := fs.Stat(fsys, "challenge.yml"); err == nil {
		s, ps := loadOne(fsys, "challenge.yml")
		if len(ps) > 0 {
			return nil, ps
		}
		return []*Spec{s}, nil
	}
	return Load(fsys, ".")
}

// Plan is what an import would do, worked out before doing it.
type Plan struct {
	New       []string
	Changed   []Change
	Unchanged []string
	// Missing are challenges in the database that the folder doesn't have.
	Missing []Missing
}

// Change is an existing challenge the import would update.
type Change struct {
	Slug string
	What []string // e.g. "value", "flags", "description"
}

// Missing is a challenge an import with prune would delete (no solves) or
// retire (solves, so the points stay).
type Missing struct {
	ID     int64
	Slug   string
	State  string
	Solves int
}

// Scored reports whether the change touches flags or points, which players
// notice mid-event.
func (c Change) Scored() bool {
	return slices.Contains(c.What, "flags") || slices.Contains(c.What, "value")
}

// MakePlan compares specs with the database. secret is the flag key, so
// exact flags can be compared by hash.
func MakePlan(ctx context.Context, db *store.Store, secret []byte, specs []*Spec) (Plan, error) {
	var p Plan
	var current []store.Challenge
	if err := db.DB.NewSelect().Model(&current).Order("position", "id").Scan(ctx); err != nil {
		return p, err
	}
	bySlug := make(map[string]*store.Challenge, len(current))
	for i := range current {
		bySlug[current[i].Slug] = &current[i]
	}
	inFolder := map[string]bool{}
	for _, s := range specs {
		inFolder[s.Slug] = true
		c := bySlug[s.Slug]
		if c == nil {
			p.New = append(p.New, s.Slug)
			continue
		}
		what, err := diff(ctx, db.DB, secret, s, c)
		if err != nil {
			return p, err
		}
		if len(what) == 0 {
			p.Unchanged = append(p.Unchanged, s.Slug)
		} else {
			p.Changed = append(p.Changed, Change{Slug: s.Slug, What: what})
		}
	}
	for _, c := range current {
		if inFolder[c.Slug] {
			continue
		}
		n, err := db.DB.NewSelect().Model((*store.Solve)(nil)).Where("challenge_id = ?", c.ID).Count(ctx)
		if err != nil {
			return p, err
		}
		p.Missing = append(p.Missing, Missing{ID: c.ID, Slug: c.Slug, State: c.State, Solves: int(n)})
	}
	return p, nil
}

// diff lists what Apply would change in c.
func diff(ctx context.Context, db bun.IDB, secret []byte, s *Spec, c *store.Challenge) ([]string, error) {
	want := *c
	s.fill(&want)
	var what []string
	add := func(changed bool, name string) {
		if changed {
			what = append(what, name)
		}
	}
	add(want.Name != c.Name, "name")
	add(want.Category != c.Category, "category")
	add(deref(want.Difficulty) != deref(c.Difficulty), "difficulty")
	add(want.Author != c.Author, "author")
	add(want.DescriptionMD != c.DescriptionMD, "description")
	add(deref(want.Connection) != deref(c.Connection), "connection")
	add(want.Value != c.Value || !intPtrEqual(want.MinValue, c.MinValue) || !intPtrEqual(want.Decay, c.Decay), "value")
	add(want.State != c.State, "state")
	add(!timeEqual(want.VisibleFrom, c.VisibleFrom), "visible_from")
	add(want.Tags != c.Tags, "tags")
	add(want.Requires != c.Requires, "requires")
	add(want.ThemeFields != c.ThemeFields, "theme_fields")
	add(deref(want.Service) != deref(c.Service), "service")

	var fl []store.Flag
	if err := db.NewSelect().Model(&fl).Where("challenge_id = ?", c.ID).Scan(ctx); err != nil {
		return nil, err
	}
	var have, need []string
	for _, f := range fl {
		have = append(have, f.Kind+"\x00"+deref(f.ValueHash)+deref(f.Pattern)+fmt.Sprint(f.TeamID != nil))
	}
	for _, f := range s.Flags {
		v := f.Value
		if f.Type != flags.Pattern {
			v = flags.Hash(secret, f.Type, f.Value)
		}
		need = append(need, f.Type+"\x00"+v+"false")
	}
	add(!sameSet(have, need), "flags")

	var hints []store.Hint
	if err := db.NewSelect().Model(&hints).Where("challenge_id = ?", c.ID).Order("position", "id").Scan(ctx); err != nil {
		return nil, err
	}
	same := len(hints) == len(s.Hints)
	for i := 0; same && i < len(hints); i++ {
		same = hints[i].Cost == s.Hints[i].Cost && hints[i].ContentMD == s.hintContent[i]
	}
	add(!same, "hints")

	var files []store.File
	if err := db.NewSelect().Model(&files).Where("challenge_id = ?", c.ID).Scan(ctx); err != nil {
		return nil, err
	}
	have, need = nil, nil
	for _, f := range files {
		have = append(have, f.Name+"\x00"+f.SHA256)
	}
	for _, f := range s.files {
		need = append(need, f.name+"\x00"+f.sha256)
	}
	add(!sameSet(have, need), "files")
	return what, nil
}

// Prune deletes the missing challenges nobody solved and retires the rest,
// so points already scored stay on the board.
func Prune(ctx context.Context, db *store.Store, missing []Missing) (deleted, retired []string, err error) {
	err = db.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		for _, m := range missing {
			if m.Solves > 0 {
				if m.State == "retired" {
					continue
				}
				if _, err := tx.NewUpdate().Model((*store.Challenge)(nil)).Set("state = ?", "retired").Where("id = ?", m.ID).Exec(ctx); err != nil {
					return err
				}
				retired = append(retired, m.Slug)
				continue
			}
			if _, err := tx.NewDelete().Model((*store.Challenge)(nil)).Where("id = ?", m.ID).Exec(ctx); err != nil {
				return err
			}
			deleted = append(deleted, m.Slug)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return deleted, retired, nil
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a, b = slices.Clone(a), slices.Clone(b)
	sort.Strings(a)
	sort.Strings(b)
	return slices.Equal(a, b)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func timeEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func intPtrEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Summary is a short human description of a plan.
func (p Plan) Summary(prune bool) string {
	var b strings.Builder
	line := func(label string, slugs []string) {
		if len(slugs) > 0 {
			fmt.Fprintf(&b, "  %-10s %d: %s\n", label, len(slugs), strings.Join(slugs, ", "))
		}
	}
	line("new", p.New)
	var changed []string
	for _, c := range p.Changed {
		changed = append(changed, c.Slug+" ("+strings.Join(c.What, ", ")+")")
	}
	line("changed", changed)
	line("unchanged", p.Unchanged)
	var del, ret, keep []string
	for _, m := range p.Missing {
		switch {
		case !prune:
			keep = append(keep, m.Slug)
		case m.Solves > 0 && m.State != "retired":
			ret = append(ret, fmt.Sprintf("%s (%d solves)", m.Slug, m.Solves))
		case m.Solves == 0:
			del = append(del, m.Slug)
		}
	}
	line("delete", del)
	line("retire", ret)
	if len(keep) > 0 {
		fmt.Fprintf(&b, "  not in the folder, kept (--prune removes them): %s\n", strings.Join(keep, ", "))
	}
	return b.String()
}
