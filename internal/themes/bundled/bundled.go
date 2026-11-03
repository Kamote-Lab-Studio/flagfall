// Package bundled lists the themes compiled into the Flagfall binary. All
// 1.0 themes ship in the core (decision D-10): to add one, embed it like
// themes/starter/embed.go and list it here.
package bundled

import (
	"fmt"
	"io/fs"

	"github.com/flagfall/flagfall/internal/themes"
	amberphosphor "github.com/flagfall/flagfall/themes/amber-phosphor"
	"github.com/flagfall/flagfall/themes/starter"
)

// packages are the embedded theme folders. Amber Phosphor, the default,
// comes first.
var packages = []fs.FS{
	amberphosphor.FS,
	starter.FS,
}

// Load reads every bundled theme. A theme whose front end wasn't built is
// still returned (Built() is false); the server explains what to do.
func Load() ([]*themes.Theme, error) {
	out := make([]*themes.Theme, 0, len(packages))
	seen := map[string]bool{}
	for _, p := range packages {
		t, err := themes.Load(p)
		if err != nil {
			return nil, fmt.Errorf("load a bundled theme: %w", err)
		}
		if seen[t.Manifest.ID] {
			return nil, fmt.Errorf("two bundled themes use the id %q", t.Manifest.ID)
		}
		seen[t.Manifest.ID] = true
		out = append(out, t)
	}
	if !seen[themes.DefaultID] {
		return nil, fmt.Errorf("the default theme %q isn't bundled", themes.DefaultID)
	}
	return out, nil
}
