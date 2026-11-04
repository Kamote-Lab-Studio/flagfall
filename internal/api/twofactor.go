package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"rsc.io/qr"

	"github.com/Kamote-Lab-Studio/flagfall/internal/auth"
	"github.com/Kamote-Lab-Studio/flagfall/internal/store"
)

// Two-factor sign-in (TOTP). Optional for every account; the admin panel
// offers it to staff. After the password, sign-in returns a short-lived
// ticket, and POST /auth/sign-in/code trades it and a code for a session.

const (
	twoFactorTicketLife = 5 * time.Minute
	recoveryCodeCount   = 10
)

// twoFactorTicket proves the password was right for user, until it expires.
// It also covers the password hash, so changing the password voids it.
func (a *API) twoFactorTicket(u *store.User, now time.Time) string {
	exp := strconv.FormatInt(now.Add(twoFactorTicketLife).Unix(), 10)
	payload := strconv.FormatInt(u.ID, 10) + "." + exp
	return payload + "." + a.ticketMAC(payload, u.PasswordHash)
}

func (a *API) ticketMAC(payload, passwordHash string) string {
	m := hmac.New(sha256.New, a.cfg.Secret())
	m.Write([]byte("flagfall-2fa-ticket-v1\x00" + payload + "\x00" + passwordHash))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// userFromTicket checks a ticket and returns its user.
func (a *API) userFromTicket(ctx context.Context, ticket string, now time.Time) (*store.User, error) {
	expired := &apiError{Status: 401, Code: "ticket_expired", Message: "That took too long. Sign in again."}
	parts := strings.Split(ticket, ".")
	if len(parts) != 3 {
		return nil, expired
	}
	id, err1 := strconv.ParseInt(parts[0], 10, 64)
	exp, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil || now.Unix() > exp {
		return nil, expired
	}
	u := new(store.User)
	if err := a.store.DB.NewSelect().Model(u).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, expired
	}
	if !hmac.Equal([]byte(a.ticketMAC(parts[0]+"."+parts[1], u.PasswordHash)), []byte(parts[2])) {
		return nil, expired
	}
	return u, nil
}

// checkSecondFactor accepts a current TOTP code or an unused recovery code,
// and records the use so neither works twice.
func (a *API) checkSecondFactor(ctx context.Context, u *store.User, code string) (bool, error) {
	if u.TOTPSecret == nil {
		return false, nil
	}
	secret, err := auth.OpenTOTPSecret(a.cfg.Secret(), *u.TOTPSecret)
	if err != nil {
		return false, err
	}
	if step, ok := auth.CheckTOTP(secret, code, a.now(), u.TOTPLastStep); ok {
		// Only move forward: two requests racing with one code can't both win.
		res, err := a.store.DB.NewUpdate().Model((*store.User)(nil)).Set("totp_last_step = ?", step).
			Where("id = ? AND totp_last_step < ?", u.ID, step).Exec(ctx)
		if err != nil {
			return false, err
		}
		n, _ := res.RowsAffected()
		return n == 1, nil
	}
	hashes := recoveryHashes(u)
	h := auth.HashRecoveryCode(a.cfg.Secret(), code)
	i := slices.Index(hashes, h)
	if i < 0 {
		return false, nil
	}
	left, _ := json.Marshal(slices.Delete(hashes, i, i+1))
	res, err := a.store.DB.NewUpdate().Model((*store.User)(nil)).Set("totp_recovery = ?", string(left)).
		Where("id = ? AND totp_recovery = ?", u.ID, u.TOTPRecovery).Exec(ctx)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func recoveryHashes(u *store.User) []string {
	var out []string
	_ = json.Unmarshal([]byte(u.TOTPRecovery), &out)
	return out
}

// POST /auth/sign-in/code {ticket, code}
func (a *API) signInCode(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var in struct {
		Ticket string `json:"ticket"`
		Code   string `json:"code"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	u, err := a.userFromTicket(ctx, in.Ticket, a.now())
	if err != nil {
		return err
	}
	key := "2fa:" + strconv.FormatInt(u.ID, 10)
	if ok, wait := a.limitSignIn.Allow(key); !ok {
		return errRateLimited(wait)
	}
	if u.BannedAt != nil {
		return errForbidden("This account is suspended. Contact the organizers.")
	}
	ok, err := a.checkSecondFactor(ctx, u, in.Code)
	if err != nil {
		return err
	}
	if !ok {
		return errValidation(map[string]string{"code": "That code didn't work. Use the newest code from your app, or a recovery code."})
	}
	a.limitSignIn.Reset(key)
	a.limitSignIn.Reset("acct:" + u.EmailKey)
	if err := a.startSession(ctx, w, r, u); err != nil {
		return err
	}
	me, err := a.meView(ctx, u)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, me)
	return nil
}

// ---------------------------------------------------------------- account

type twoFactorView struct {
	Enabled           bool `json:"enabled"`
	RecoveryCodesLeft int  `json:"recoveryCodesLeft"`
}

func twoFactorOf(u *store.User) twoFactorView {
	if u.TOTPSecret == nil {
		return twoFactorView{}
	}
	return twoFactorView{Enabled: true, RecoveryCodesLeft: len(recoveryHashes(u))}
}

// reloadUser reads the signed-in user again (the session copy may be stale).
func (a *API) reloadUser(ctx context.Context, r *http.Request) (*store.User, error) {
	u := new(store.User)
	return u, a.store.DB.NewSelect().Model(u).Where("id = ?", currentUser(r).ID).Scan(ctx)
}

// GET /me/two-factor
func (a *API) getTwoFactor(w http.ResponseWriter, r *http.Request) error {
	u, err := a.reloadUser(r.Context(), r)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, twoFactorOf(u))
	return nil
}

// POST /me/two-factor/setup: a new secret to scan. Two-factor isn't on
// until /enable confirms a code from it.
func (a *API) setupTwoFactor(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	u, err := a.reloadUser(ctx, r)
	if err != nil {
		return err
	}
	if u.TOTPSecret != nil {
		return errConflict("Two-factor sign-in is already on. Turn it off first to use another device.")
	}
	secret := auth.NewTOTPSecret()
	sealed, err := auth.SealTOTPSecret(a.cfg.Secret(), secret)
	if err != nil {
		return err
	}
	if _, err := a.store.DB.NewUpdate().Model((*store.User)(nil)).Set("totp_pending = ?", sealed).Where("id = ?", u.ID).Exec(ctx); err != nil {
		return err
	}
	ev, err := a.store.LoadEvent(ctx)
	if err != nil {
		return err
	}
	link := auth.TOTPURL(ev.Name, u.Email, secret)
	svg, err := qrSVG(link)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"secret": secret, "url": link, "qrSvg": svg})
	return nil
}

// POST /me/two-factor/enable {code}: turns it on and returns the
// recovery codes, shown this once.
func (a *API) enableTwoFactor(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var in struct {
		Code string `json:"code"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	u, err := a.reloadUser(ctx, r)
	if err != nil {
		return err
	}
	if u.TOTPSecret != nil {
		return errConflict("Two-factor sign-in is already on.")
	}
	if u.TOTPPending == nil {
		return errBadRequest("Start the setup first.")
	}
	if ok, wait := a.limitSignIn.Allow("2fa:" + strconv.FormatInt(u.ID, 10)); !ok {
		return errRateLimited(wait)
	}
	secret, err := auth.OpenTOTPSecret(a.cfg.Secret(), *u.TOTPPending)
	if err != nil {
		return err
	}
	step, ok := auth.CheckTOTP(secret, in.Code, a.now(), 0)
	if !ok {
		return errValidation(map[string]string{"code": "That code didn't work. Check the time on your phone, and use the newest code."})
	}
	codes, hashes := a.newRecoveryCodes()
	if _, err := a.store.DB.NewUpdate().Model((*store.User)(nil)).
		Set("totp_secret = totp_pending").Set("totp_pending = NULL").
		Set("totp_last_step = ?", step).Set("totp_recovery = ?", hashes).
		Where("id = ? AND totp_pending IS NOT NULL", u.ID).Exec(ctx); err != nil {
		return err
	}
	a.auditSelf(ctx, r, "two-factor turned on")
	writeJSON(w, http.StatusOK, map[string]any{"recoveryCodes": codes})
	return nil
}

func (a *API) newRecoveryCodes() ([]string, string) {
	codes := auth.NewRecoveryCodes(recoveryCodeCount)
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = auth.HashRecoveryCode(a.cfg.Secret(), c)
	}
	b, _ := json.Marshal(hashes)
	return codes, string(b)
}

// confirmPassword checks the signed-in user's password for sensitive changes.
func (a *API) confirmPassword(r *http.Request, u *store.User, password string) error {
	if ok, wait := a.limitSignIn.Allow("acct:" + u.EmailKey); !ok {
		return errRateLimited(wait)
	}
	if ok, _ := auth.CheckPassword(u.PasswordHash, password); !ok {
		return errValidation(map[string]string{"password": "That isn't your password."})
	}
	return nil
}

// POST /me/two-factor/disable {password}
func (a *API) disableTwoFactor(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var in struct {
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	u, err := a.reloadUser(ctx, r)
	if err != nil {
		return err
	}
	if err := a.confirmPassword(r, u, in.Password); err != nil {
		return err
	}
	if err := a.clearTwoFactor(ctx, u.ID); err != nil {
		return err
	}
	a.auditSelf(ctx, r, "two-factor turned off")
	return noContent(w)
}

// POST /me/two-factor/recovery-codes {password}: replaces them all.
func (a *API) regenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var in struct {
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	u, err := a.reloadUser(ctx, r)
	if err != nil {
		return err
	}
	if u.TOTPSecret == nil {
		return errConflict("Two-factor sign-in is off.")
	}
	if err := a.confirmPassword(r, u, in.Password); err != nil {
		return err
	}
	codes, hashes := a.newRecoveryCodes()
	if _, err := a.store.DB.NewUpdate().Model((*store.User)(nil)).Set("totp_recovery = ?", hashes).Where("id = ?", u.ID).Exec(ctx); err != nil {
		return err
	}
	a.auditSelf(ctx, r, "new two-factor recovery codes")
	writeJSON(w, http.StatusOK, map[string]any{"recoveryCodes": codes})
	return nil
}

func (a *API) clearTwoFactor(ctx context.Context, userID int64) error {
	_, err := a.store.DB.NewUpdate().Model((*store.User)(nil)).
		Set("totp_secret = NULL").Set("totp_pending = NULL").Set("totp_recovery = '[]'").
		Where("id = ?", userID).Exec(ctx)
	return err
}

// auditSelf records a staff member's own two-factor changes (players'
// accounts aren't audited).
func (a *API) auditSelf(ctx context.Context, r *http.Request, what string) {
	u := currentUser(r)
	if u == nil || u.Role == "player" {
		return
	}
	target := "user=" + strconv.FormatInt(u.ID, 10)
	a.writeAudit(ctx, r, u.ID, what, &target, nil)
}

// qrSVG draws text as a QR code, black on white, as an SVG document.
func qrSVG(text string) (string, error) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const quiet = 4
	n := c.Size + 2*quiet
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, n, n, n, n)
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; x++ {
			if c.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String(), nil
}
