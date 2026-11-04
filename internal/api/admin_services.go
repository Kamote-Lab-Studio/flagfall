package api

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Kamote-Lab-Studio/flagfall/internal/services"
)

// Challenge containers: the `service:` block in challenge.yml. Support staff
// can see them; admins start, stop and restart them.
func (a *API) mountAdminServices(r chi.Router) {
	r.Get("/services", a.h(a.staff("support", a.adminListServices)))
	r.Post("/services/{slug}/{action}", a.h(a.staff("admin", a.adminServiceAction)))
}

// servicesOffMessage explains how to turn challenge containers on.
const servicesOffMessage = "Challenge containers are off: set FLAGFALL_DOCKER_HOST so Flagfall can reach Docker (see docs/dev/challenge-services.md)."

func (a *API) adminListServices(w http.ResponseWriter, r *http.Request) error {
	list, err := a.services.List(r.Context())
	if err != nil {
		return err
	}
	out := map[string]any{"enabled": a.services.Enabled(), "services": list}
	if !a.services.Enabled() {
		out["message"] = servicesOffMessage
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) adminServiceAction(w http.ResponseWriter, r *http.Request) error {
	if !a.services.Enabled() {
		return errConflict(servicesOffMessage)
	}
	ctx, slug := r.Context(), chi.URLParam(r, "slug")
	var err error
	switch chi.URLParam(r, "action") {
	case "start":
		err = a.services.SetDesired(ctx, slug, services.DesiredRunning)
	case "stop":
		err = a.services.SetDesired(ctx, slug, services.DesiredStopped)
	case "restart":
		err = a.services.Restart(ctx, slug)
	default:
		return errNotFound
	}
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return errNotFound
	case errors.Is(err, services.ErrNoService):
		return errConflict("This challenge has no service block in its challenge.yml.")
	case err != nil:
		return err
	}
	return a.adminListServices(w, r)
}
