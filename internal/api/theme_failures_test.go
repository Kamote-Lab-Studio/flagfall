package api_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/flagfall/flagfall/internal/themes"
)

func TestThemeFailureReports(t *testing.T) {
	e, _ := themeEnv(t)
	admin := e.staff("ada", "admin")
	player := e.signUp("pat")
	anon := e.client()

	admin.must(200, "POST", "/admin/api/v1/themes/neon/publish", nil)
	version := int(findTheme(admin.must(200, "GET", "/admin/api/v1/themes", nil), "neon")["published"].(map[string]any)["version"].(float64))

	// Reports about themes players aren't getting are ignored.
	anon.must(204, "POST", "/api/v1/theme/failures", map[string]any{"themeId": "neon", "version": version + 7, "reason": "old"})
	anon.must(204, "POST", "/api/v1/theme/failures", map[string]any{"themeId": "other", "version": 1, "reason": "made up"})
	if l := admin.must(200, "GET", "/admin/api/v1/themes", nil); len(l.body["browserFailures"].([]any)) != 0 {
		t.Fatalf("ignored reports were kept: %s", l.raw)
	}

	// A report for the live version is a warning on Appearance and the dashboard.
	anon.must(204, "POST", "/api/v1/theme/failures", map[string]any{
		"themeId": "neon", "version": version, "reason": "TypeError: x is undefined", "path": "/scoreboard"})
	l := admin.must(200, "GET", "/admin/api/v1/themes", nil)
	fs := l.body["browserFailures"].([]any)
	if len(fs) != 1 || fs[0].(map[string]any)["clients"] != float64(1) || fs[0].(map[string]any)["lastReason"] != "TypeError: x is undefined" {
		t.Fatalf("failures = %s", l.raw)
	}
	want := "The Neon theme didn't start in 1 player's browser in the last hour"
	if !strings.Contains(string(l.raw), want) || !strings.Contains(string(admin.must(200, "GET", "/admin/api/v1/dashboard", nil).raw), want) {
		t.Fatalf("no warning: %s", l.raw)
	}

	// A browser with the fallback cookie for this version gets the default
	// theme; nobody else does.
	u, _ := url.Parse(e.server.URL)
	player.http.Jar.SetCookies(u, []*http.Cookie{{Name: themes.FallbackCookie, Value: url.QueryEscape("neon:" + strconv.Itoa(version)), Path: "/"}})
	if th := player.must(200, "GET", "/api/v1/theme", nil); th.str("id") != "amber-phosphor" {
		t.Fatalf("with the cookie: %s", th.raw)
	}
	if th := anon.must(200, "GET", "/api/v1/theme", nil); th.str("id") != "neon" {
		t.Fatalf("without the cookie: %s", th.raw)
	}
	// Publishing again (a fix) makes the old cookie stale.
	admin.must(200, "POST", "/admin/api/v1/themes/neon/publish", nil)
	if th := player.must(200, "GET", "/api/v1/theme", nil); th.str("id") != "neon" {
		t.Fatalf("after a new version: %s", th.raw)
	}

	// Reports are rate-limited per address.
	for i := 0; i < 10; i++ {
		if r := anon.do("POST", "/api/v1/theme/failures", map[string]any{"themeId": "x", "version": 1}); r.status == 429 {
			return
		}
	}
	t.Fatal("reports aren't rate-limited")
}
