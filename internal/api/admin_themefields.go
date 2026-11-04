package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Kamote-Lab-Studio/flagfall/internal/store"
	"github.com/Kamote-Lab-Studio/flagfall/internal/themes"
)

// activeFields returns the active theme's id, name and challenge fields.
func (a *API) activeFields() (id, name string, fields []themes.ChallengeField) {
	if a.theme == nil {
		return "", "", nil
	}
	t := a.theme()
	if t == nil {
		return "", "", nil
	}
	return t.Manifest.ID, t.Manifest.Name, t.Manifest.ChallengeFields
}

// adminChallengeFields lists the fields the challenge form shows.
func (a *API) adminChallengeFields(w http.ResponseWriter, r *http.Request) error {
	id, name, fields := a.activeFields()
	if fields == nil {
		fields = []themes.ChallengeField{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"theme": map[string]string{"id": id, "name": name}, "fields": fields})
	return nil
}

// themeFieldWarnings says which required fields are missing, and which
// stored keys the active theme doesn't use.
func (a *API) themeFieldWarnings(c *store.Challenge) (warnings, other []string) {
	_, _, fields := a.activeFields()
	var values map[string]any
	_ = json.Unmarshal([]byte(c.ThemeFields), &values)
	r := themes.CheckFields(fields, values)
	warnings, other = r.Warnings(fields), r.Unknown
	if warnings == nil {
		warnings = []string{}
	}
	if other == nil {
		other = []string{}
	}
	return warnings, other
}

// missingFieldsWarning is a dashboard line for a challenge that lacks
// fields the active theme requires, or "".
func (a *API) missingFieldsWarning(c *store.Challenge) string {
	_, name, fields := a.activeFields()
	var values map[string]any
	_ = json.Unmarshal([]byte(c.ThemeFields), &values)
	missing := themes.CheckFields(fields, values).Missing
	if len(missing) == 0 {
		return ""
	}
	labels := make([]string, len(missing))
	for i, k := range missing {
		for _, f := range fields {
			if f.Key == k {
				labels[i] = f.Label
			}
		}
	}
	return fmt.Sprintf("%s is missing %s, which the %s theme needs.", c.Name, strings.Join(labels, ", "), name)
}
