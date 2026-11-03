package api_test

import (
	"context"
	"strings"
	"testing"

	"github.com/flagfall/flagfall/internal/api"
	"github.com/flagfall/flagfall/internal/services"
	"github.com/flagfall/flagfall/internal/store"
)

// withService gives a challenge a stored service spec.
func (e *env) withService(slug string) {
	e.t.Helper()
	spec := `{"image":"busybox:1.36","port":8080,"cpu":0.5,"memoryMB":256,"healthcheck":"tcp"}`
	if _, err := e.store.DB.NewUpdate().Model((*store.Challenge)(nil)).Set("service = ?", spec).Where("slug = ?", slug).Exec(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

func TestServicesOffWithoutDocker(t *testing.T) {
	e := newEnv(t)
	e.addChallenge("web", 100, "flag{w}")
	e.withService("web")
	admin := e.staff("ada", "admin")

	list := admin.must(200, "GET", "/admin/api/v1/services", nil)
	if list.body["enabled"] != false || !strings.Contains(list.str("message"), "FLAGFALL_DOCKER_HOST") {
		t.Fatalf("services = %s", list.raw)
	}
	svcs := list.body["services"].([]any)
	if len(svcs) != 1 || svcs[0].(map[string]any)["state"] != "stopped" || svcs[0].(map[string]any)["image"] != "busybox:1.36" {
		t.Fatalf("services = %s", list.raw)
	}
	if r := admin.must(409, "POST", "/admin/api/v1/services/web/start", nil); !strings.Contains(r.str("error", "message"), "FLAGFALL_DOCKER_HOST") {
		t.Fatalf("start = %s", r.raw)
	}
	if d := admin.must(200, "GET", "/admin/api/v1/dashboard", nil); d.body["servicesEnabled"] != false || len(d.body["services"].([]any)) != 1 {
		t.Fatalf("dashboard = %s", d.raw)
	}
}

func TestServiceActions(t *testing.T) {
	var runner *services.Runner
	e := makeEnv(t, true, func(o *api.Options) {
		// Nothing calls Docker here: the runner loop isn't started.
		d, err := services.NewDocker("tcp://127.0.0.1:1")
		if err != nil {
			t.Fatal(err)
		}
		runner = services.New(services.Options{Store: o.Store, Docker: d})
		o.Services = runner
	})
	e.addChallenge("web", 100, "flag{w}")
	e.addChallenge("plain", 100, "flag{p}")
	e.withService("web")
	admin := e.staff("ada", "admin")
	support := e.staff("sue", "support")

	support.must(200, "GET", "/admin/api/v1/services", nil)
	support.must(403, "POST", "/admin/api/v1/services/web/start", nil)
	admin.must(404, "POST", "/admin/api/v1/services/nope/start", nil)
	admin.must(404, "POST", "/admin/api/v1/services/web/explode", nil)
	admin.must(409, "POST", "/admin/api/v1/services/plain/start", nil)

	r := admin.must(200, "POST", "/admin/api/v1/services/web/start", nil)
	web := r.body["services"].([]any)[0].(map[string]any)
	if r.body["enabled"] != true || web["desired"] != "running" || web["state"] != "starting" {
		t.Fatalf("start = %s", r.raw)
	}
	if last := e.audit(); len(last) == 0 || !strings.Contains(last[len(last)-1].Action, "services") {
		t.Fatalf("audit = %+v", last)
	}

	// A failed service shows up as a dashboard warning.
	if _, err := e.store.DB.NewUpdate().Model((*store.ChallengeService)(nil)).
		Set("status = ?", services.StateFailed).Set("message = ?", "the image didn't build").Where("1 = 1").Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	d := admin.must(200, "GET", "/admin/api/v1/dashboard", nil)
	if !strings.Contains(string(d.raw), "WEB's container failed: the image didn't build") {
		t.Fatalf("dashboard = %s", d.raw)
	}

	r = admin.must(200, "POST", "/admin/api/v1/services/web/stop", nil)
	if web = r.body["services"].([]any)[0].(map[string]any); web["desired"] != "stopped" || web["state"] != "stopped" {
		t.Fatalf("stop = %s", r.raw)
	}
	admin.must(200, "POST", "/admin/api/v1/services/web/restart", nil)
}
