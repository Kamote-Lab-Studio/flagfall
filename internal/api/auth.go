package api

import (
	"context"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/flagfall/flagfall/internal/auth"
	"github.com/flagfall/flagfall/internal/store"
)

func validName(name string) string {
	n := utf8.RuneCountInString(name)
	switch {
	case n < 2:
		return "Use at least 2 characters."
	case n > 32:
		return "Use at most 32 characters."
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "Remove control characters."
		}
	}
	return ""
}

func nameKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

func (a *API) signUp(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var in struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Country  string `json:"country"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	ev, err := a.store.LoadEvent(ctx)
	if err != nil {
		return err
	}
	if !ev.RegistrationOpen {
		return &apiError{Status: 403, Code: "registration_closed", Message: "Registration is closed."}
	}
	if ok, wait := a.limitSignUp.Allow(clientIP(r)); !ok {
		return errRateLimited(wait)
	}

	user, err := a.newAccount(ctx, a.store.DB, in.Name, in.Email, in.Password, in.Country, "player")
	if err != nil {
		return err
	}
	if err := a.startSession(ctx, w, r, user); err != nil {
		return err
	}
	me, err := a.meView(ctx, user)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, me)
	return nil
}

func (a *API) signIn(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	emailKey := strings.ToLower(strings.TrimSpace(in.Email))
	ipKey, acctKey := "ip:"+clientIP(r), "acct:"+emailKey
	if ok, wait := a.limitSignIn.Allow(ipKey); !ok {
		return errRateLimited(wait)
	}
	if ok, wait := a.limitSignIn.Allow(acctKey); !ok {
		return errRateLimited(wait)
	}

	user := new(store.User)
	err := a.store.DB.NewSelect().Model(user).Where("email_key = ?", emailKey).Scan(ctx)
	hash := ""
	if err == nil {
		hash = user.PasswordHash
	}
	if !auth.CheckPasswordOrDummy(hash, in.Password) {
		return &apiError{Status: 401, Code: "unauthenticated", Message: "Wrong email or password."}
	}
	if user.BannedAt != nil {
		return errForbidden("This account is suspended. Contact the organizers.")
	}
	a.limitSignIn.Reset(acctKey)
	if err := a.startSession(ctx, w, r, user); err != nil {
		return err
	}
	me, err := a.meView(ctx, user)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, me)
	return nil
}

func (a *API) signOut(w http.ResponseWriter, r *http.Request) error {
	if h := currentSessionHash(r); h != "" {
		if _, err := a.store.DB.NewDelete().Model((*store.Session)(nil)).Where("id_hash = ?", h).Exec(r.Context()); err != nil {
			return err
		}
	}
	a.clearSessionCookie(w)
	return noContent(w)
}

func (a *API) getMe(w http.ResponseWriter, r *http.Request) error {
	user := currentUser(r)
	if user == nil {
		writeJSON(w, http.StatusOK, nil)
		return nil
	}
	me, err := a.meView(r.Context(), user)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, me)
	return nil
}

func (a *API) updateMe(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	user := currentUser(r)
	var in struct {
		Name    *string `json:"name"`
		Country *string `json:"country"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	q := a.store.DB.NewUpdate().Model(user).WherePK()
	fields := map[string]string{}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if msg := validName(name); msg != "" {
			fields["name"] = msg
		} else if n, _ := a.store.DB.NewSelect().Model((*store.User)(nil)).Where("name_key = ? AND id <> ?", nameKey(name), user.ID).Count(ctx); n > 0 {
			fields["name"] = "That name is taken."
		} else {
			user.Name, user.NameKey = name, nameKey(name)
			q = q.Column("name", "name_key")
		}
	}
	if in.Country != nil {
		c, ok := normalizeCountry(*in.Country)
		if !ok {
			fields["country"] = "Use a two-letter country code."
		} else {
			user.Country = strp(c)
			q = q.Column("country")
		}
	}
	if len(fields) > 0 {
		return errValidation(fields)
	}
	if in.Name != nil || in.Country != nil {
		if _, err := q.Exec(ctx); err != nil {
			return err
		}
		a.boards.Invalidate()
	}
	me, err := a.meView(ctx, user)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, me)
	return nil
}

func (a *API) changePassword(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	user := currentUser(r)
	var in struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if ok, wait := a.limitSignIn.Allow("acct:" + user.EmailKey); !ok {
		return errRateLimited(wait)
	}
	if ok, _ := auth.CheckPassword(user.PasswordHash, in.Current); !ok {
		return errValidation(map[string]string{"current": "That isn't your current password."})
	}
	if msg := auth.ValidatePassword(in.Next); msg != "" {
		return errValidation(map[string]string{"next": msg})
	}
	if err := a.setPassword(ctx, user.ID, in.Next, currentSessionHash(r)); err != nil {
		return err
	}
	return noContent(w)
}

// setPassword changes a password and ends every session except keep.
func (a *API) setPassword(ctx context.Context, userID int64, password, keep string) error {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	return a.store.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewUpdate().Model((*store.User)(nil)).Set("password_hash = ?", hash).Where("id = ?", userID).Exec(ctx); err != nil {
			return err
		}
		_, err := tx.NewDelete().Model((*store.Session)(nil)).Where("user_id = ? AND id_hash <> ?", userID, keep).Exec(ctx)
		return err
	})
}

const resetLifetime = time.Hour

func (a *API) requestPasswordReset(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var in struct {
		Email string `json:"email"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	emailKey := strings.ToLower(strings.TrimSpace(in.Email))
	if ok, wait := a.limitReset.Allow("ip:" + clientIP(r)); !ok {
		return errRateLimited(wait)
	}
	if ok, _ := a.limitReset.Allow("email:" + emailKey); !ok {
		return noContent(w) // don't reveal anything; just stop sending
	}
	user := new(store.User)
	if err := a.store.DB.NewSelect().Model(user).Where("email_key = ?", emailKey).Scan(ctx); err != nil || user.BannedAt != nil {
		return noContent(w) // same answer whether or not the account exists
	}
	token := auth.RandomToken(32)
	pr := &store.PasswordReset{TokenHash: hashToken(token), UserID: user.ID, ExpiresAt: a.now().UTC().Add(resetLifetime)}
	if _, err := a.store.DB.NewInsert().Model(pr).Exec(ctx); err != nil {
		return err
	}
	link := a.cfg.PublicURL + "/reset-password?token=" + token
	body := "Someone asked to reset the password for your account.\n\nTo choose a new password, open this link within one hour:\n" + link + "\n\nIf this wasn't you, ignore this email."
	if err := a.mailer.Send(ctx, user.Email, "Reset your password", body); err != nil {
		a.log.Error("send password reset email", "err", err)
	}
	return noContent(w)
}

func (a *API) confirmPasswordReset(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if msg := auth.ValidatePassword(in.Password); msg != "" {
		return errValidation(map[string]string{"password": msg})
	}
	invalid := errValidation(map[string]string{"token": "This reset link is invalid or has expired. Request a new one."})
	pr := new(store.PasswordReset)
	if err := a.store.DB.NewSelect().Model(pr).Where("token_hash = ?", hashToken(in.Token)).Scan(ctx); err != nil {
		return invalid
	}
	now := a.now().UTC()
	if pr.UsedAt != nil || now.After(pr.ExpiresAt) {
		return invalid
	}
	res, err := a.store.DB.NewUpdate().Model(pr).Set("used_at = ?", now).WherePK().Where("used_at IS NULL").Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return invalid // used concurrently
	}
	if err := a.setPassword(ctx, pr.UserID, in.Password, ""); err != nil {
		return err
	}
	return noContent(w)
}

// newAccount validates and inserts a user with the given role. It returns a
// validation error listing every problem at once.
func (a *API) newAccount(ctx context.Context, db bun.IDB, name, email, password, countryIn, role string) (*store.User, error) {
	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
	fields := map[string]string{}
	if msg := validName(name); msg != "" {
		fields["name"] = msg
	}
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email || len(email) > 254 {
		fields["email"] = "Enter a valid email address."
	}
	if msg := auth.ValidatePassword(password); msg != "" {
		fields["password"] = msg
	}
	country, ok := normalizeCountry(countryIn)
	if !ok {
		fields["country"] = "Use a two-letter country code."
	}
	if fields["name"] == "" {
		if n, _ := db.NewSelect().Model((*store.User)(nil)).Where("name_key = ?", nameKey(name)).Count(ctx); n > 0 {
			fields["name"] = "That name is taken."
		}
	}
	if fields["email"] == "" {
		if n, _ := db.NewSelect().Model((*store.User)(nil)).Where("email_key = ?", strings.ToLower(email)).Count(ctx); n > 0 {
			fields["email"] = "An account with this email already exists. Sign in instead."
		}
	}
	if len(fields) > 0 {
		return nil, errValidation(fields)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	user := &store.User{
		Name: name, NameKey: nameKey(name), Email: email, EmailKey: strings.ToLower(email),
		PasswordHash: hash, Role: role, Country: strp(country), CreatedAt: a.now().UTC(),
	}
	if _, err := db.NewInsert().Model(user).Exec(ctx); err != nil {
		// Lost a race with another sign-up for the same name or email.
		return nil, errValidation(map[string]string{"name": "That name or email is taken."})
	}
	return user, nil
}
