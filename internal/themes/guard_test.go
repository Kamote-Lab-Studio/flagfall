package themes_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/store/storetest"
	"github.com/flagfall/flagfall/internal/themes"
)

func TestGuardAndBrowserFallback(t *testing.T) {
	db := storetest.New(t)
	def, neon := demoTheme(t, "amber-phosphor"), demoTheme(t, "neon")
	for _, th := range []*themes.Theme{def, neon} {
		if err := th.Register(t.Context(), db, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	if _, err := db.DB.NewInsert().Model(&store.ThemeSettings{ThemeID: "neon", Version: 3, Status: "live", Data: "{}", PublishedAt: &now}).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	srv := newServerWith(t, handler(db, def, neon))

	// The guard is served, and added before anything else in <head>.
	res, js := get(t, srv.URL+themes.GuardPath)
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/javascript") || !strings.Contains(js, themes.FallbackCookie) {
		t.Fatalf("guard: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	_, page := get(t, srv.URL+"/scoreboard")
	if !strings.Contains(page, `<head><script src="/theme-guard.js" data-theme="neon" data-version="3"></script><title>`) {
		t.Fatalf("page = %s", page)
	}

	fetch := func(cookie string) string {
		req, _ := http.NewRequest("GET", srv.URL+"/", nil)
		req.AddCookie(&http.Cookie{Name: themes.FallbackCookie, Value: cookie})
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}
	// A browser that saw neon:3 fail gets the default theme, which only reports.
	if p := fetch("neon%3A3"); !strings.Contains(p, `data-theme="amber-phosphor" data-version="0" data-default`) {
		t.Fatalf("with the cookie: %s", p)
	}
	// A cookie for another version (or junk) changes nothing.
	for _, c := range []string{"neon%3A2", "amber-phosphor%3A0", "%zz"} {
		if p := fetch(c); !strings.Contains(p, `data-theme="neon"`) {
			t.Fatalf("cookie %q: %s", c, p)
		}
	}
}

func TestInjectHeadStart(t *testing.T) {
	for in, want := range map[string]string{
		`<html><HEAD lang="x"><title>`: `<html><HEAD lang="x">X<title>`,
		`<p>no head</p>`:               `X<p>no head</p>`,
	} {
		if got := string(themes.InjectHeadStart([]byte(in), "X")); got != want {
			t.Errorf("InjectHeadStart(%q) = %q, want %q", in, got, want)
		}
	}
}
