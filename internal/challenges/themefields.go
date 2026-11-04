package challenges

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/Kamote-Lab-Studio/flagfall/internal/themes"
)

// ThemeFieldCheck is the result of CheckThemeFields.
type ThemeFieldCheck struct {
	// Problems are values of the wrong type: the import must not go ahead.
	Problems Problems
	// Warnings are required fields that challenges leave unset.
	Warnings []Problem
	// Unused maps keys the theme doesn't use to the challenges that set
	// them. They're kept, for themes that do use them.
	Unused map[string][]string
}

// CheckThemeFields checks theme_fields against the active theme's
// challenge fields.
func CheckThemeFields(specs []*Spec, fields []themes.ChallengeField) ThemeFieldCheck {
	res := ThemeFieldCheck{Unused: map[string][]string{}}
	for _, s := range specs {
		file := path.Join(s.dir, "challenge.yml")
		r := themes.CheckFields(fields, s.ThemeFields)
		keys := make([]string, 0, len(r.Invalid))
		for k := range r.Invalid {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			res.Problems = append(res.Problems, Problem{file, "theme_fields." + k + ": " + r.Invalid[k]})
		}
		for _, w := range r.Warnings(fields) {
			res.Warnings = append(res.Warnings, Problem{file, w})
		}
		for _, k := range r.Unknown {
			res.Unused[k] = append(res.Unused[k], s.Slug)
		}
	}
	return res
}

// UnusedNote is one line about theme_fields keys the theme ignores, or "".
func (c ThemeFieldCheck) UnusedNote(themeName string) string {
	if len(c.Unused) == 0 {
		return ""
	}
	keys := make([]string, 0, len(c.Unused))
	for k, slugs := range c.Unused {
		keys = append(keys, fmt.Sprintf("%s (%d challenge(s))", k, len(slugs)))
	}
	sort.Strings(keys)
	return fmt.Sprintf("The %s theme doesn't use theme_fields %s. They're kept for themes that do.", themeName, strings.Join(keys, ", "))
}
