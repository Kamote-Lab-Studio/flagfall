package api

import (
	"fmt"
	"net/http"

	"github.com/Kamote-Lab-Studio/flagfall/internal/themes"
)

// reportThemeFailure records a player's browser saying the theme didn't
// start (see internal/themes/guard.js). Anyone can report, so reports are
// rate-limited per address, only count for the theme players are getting,
// and only raise warnings; they never switch the theme for anyone else.
func (a *API) reportThemeFailure(w http.ResponseWriter, r *http.Request) error {
	if a.themes == nil {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	ip := clientIP(r)
	if ok, wait := a.limitReports.Allow("theme-failure:" + ip); !ok {
		return errRateLimited(wait)
	}
	var in struct {
		ThemeID string `json:"themeId"`
		Version int    `json:"version"`
		Reason  string `json:"reason"`
		Path    string `json:"path"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	a.themes.ReportFailure(r, in.ThemeID, in.Version, in.Reason, in.Path, ip)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// themeFailureWarnings explains browser reports from the last hour.
func (a *API) themeFailureWarnings() []string {
	if a.themes == nil {
		return nil
	}
	var out []string
	for _, f := range a.themes.Failures() {
		name := f.ThemeID
		if t := a.themes.Registry.Get(f.ThemeID); t != nil {
			name = t.Manifest.Name
		}
		who := "1 player's browser"
		if f.Clients != 1 {
			who = fmt.Sprintf("%d players' browsers", f.Clients)
		}
		reason := f.Reason
		if f.Path != "" {
			reason += " (on " + f.Path + ")"
		}
		if f.ThemeID == themes.DefaultID {
			out = append(out, fmt.Sprintf("The %s theme didn't start in %s in the last hour: %s", name, who, reason))
			continue
		}
		out = append(out, fmt.Sprintf("The %s theme didn't start in %s in the last hour, so they got the default theme for 15 minutes. Last error: %s. Preview the theme in Appearance to see what's wrong, or publish another one.", name, who, reason))
	}
	return out
}

func orEmptyStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
