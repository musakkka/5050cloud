// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/core/auth"
	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/ids"
	"github.com/musakkka/5050cloud/internal/cp/core/mail"
	"github.com/musakkka/5050cloud/internal/cp/core/store"
	"github.com/musakkka/5050cloud/pkg/api"
)

// Session and token lifetimes (9.15).
const (
	SessionTTL     = 30 * day
	sessionMaxAge  = 90 * day // however often it is used, a session ends after this
	sessionExtend  = day      // extend the sliding expiry at most once a day
	ResetTTL       = hour
	VerifyTTL      = 7 * day
	SetupTTL       = day
	TicketTTL      = 5 * time.Minute
	CLIAuthTTL     = 10 * time.Minute
	InviteDefaultH = 168
)

// userRow is a users row.
type userRow struct {
	api.User
	PasswordHash  string
	TOTPSecretEnc []byte
	RecoveryHash  []string
}

const userCols = `id, email, name, password_hash, role, email_verified_at, totp_secret_enc, totp_enabled, recovery_codes_hash, created_at, disabled`

func scanUser(r interface{ Scan(...any) error }) (userRow, error) {
	var u userRow
	var verified sql.NullInt64
	var totpOn, disabled int
	var rec string
	err := r.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &verified, &u.TOTPSecretEnc, &totpOn, &rec, &u.CreatedAt, &disabled)
	if err != nil {
		return u, err
	}
	u.EmailVerified = verified.Valid
	u.TOTPEnabled = totpOn == 1
	u.Disabled = disabled == 1
	_ = json.Unmarshal([]byte(rec), &u.RecoveryHash)
	return u, nil
}

func (c *Core) userByID(ctx context.Context, q store.Querier, id string) (userRow, error) {
	u, err := scanUser(q.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
	if store.NoRows(err) {
		return u, fail.NotFound("The user %s does not exist.", id)
	}
	return u, err
}

func (c *Core) userByEmail(ctx context.Context, q store.Querier, email string) (userRow, bool, error) {
	u, err := scanUser(q.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE email = ?`, normEmail(email)))
	if store.NoRows(err) {
		return u, false, nil
	}
	return u, err == nil, err
}

func totpAAD(userID string) string { return "users|" + userID + "|totp_secret_enc" }

// User returns one user.
func (c *Core) User(ctx context.Context, id string) (api.User, error) {
	u, err := c.userByID(ctx, c.st.Reader, id)
	return u.User, err
}

// Me returns the user and the install.
func (c *Core) Me(ctx context.Context, userID string) (api.Me, error) {
	u, err := c.User(ctx, userID)
	if err != nil {
		return api.Me{}, err
	}
	in, err := c.Install(ctx)
	return api.Me{User: u, Install: in}, err
}

// ---------------------------------------------------------------- setup

// SetupStatus is GET /setup.
func (c *Core) SetupStatus(ctx context.Context) (api.SetupStatus, error) {
	var n int
	if err := c.st.Reader.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return api.SetupStatus{}, err
	}
	return api.SetupStatus{NeedsSetup: n == 0, Version: versionString()}, nil
}

// CreateSetupToken replaces unused setup tokens with a new one (24 hours).
// It is used by `5050d init` and `5050d setup-token`.
func (c *Core) CreateSetupToken(ctx context.Context) (string, error) {
	t := auth.NewSetupToken()
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fail.Conflict("This install already has an owner, so it needs no setup token. Invite people from Settings > Members instead.")
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM setup_tokens WHERE used_at IS NULL`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO setup_tokens (token_hash, expires_at) VALUES (?, ?)`, auth.HashToken(auth.NormalizeCode(t)), c.Now()+SetupTTL)
		return err
	})
	return t, err
}

// Setup creates the owner with a setup token.
func (c *Core) Setup(ctx context.Context, req api.SetupRequest) (api.User, error) {
	email := normEmail(req.Email)
	if !validEmail(email) {
		return api.User{}, fail.Validation("email", "Enter a valid email address.")
	}
	if err := auth.CheckPasswordPolicy("password", req.Password); err != nil {
		return api.User{}, err
	}
	// Refuse a wrong token before the 64 MiB hash; the transaction checks it fully.
	var known int
	if err := c.st.Reader.QueryRowContext(ctx, `SELECT count(*) FROM setup_tokens WHERE token_hash = ?`, auth.HashToken(auth.NormalizeCode(req.Token))).Scan(&known); err != nil {
		return api.User{}, err
	}
	if known == 0 {
		return api.User{}, fail.Unauthorized("This setup link is not valid. Run `sudo 5050d setup-token` on the control host for a new one.")
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return api.User{}, err
	}
	var u api.User
	err = c.st.Write(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fail.Conflict("Setup is already complete. Log in instead.")
		}
		var exp int64
		var used sql.NullInt64
		h := auth.HashToken(auth.NormalizeCode(req.Token))
		err := tx.QueryRowContext(ctx, `SELECT expires_at, used_at FROM setup_tokens WHERE token_hash = ?`, h).Scan(&exp, &used)
		switch {
		case store.NoRows(err):
			return fail.Unauthorized("This setup link is not valid. Run `sudo 5050d setup-token` on the control host for a new one.")
		case err != nil:
			return err
		case used.Valid:
			return fail.Unauthorized("This setup link was already used. Run `sudo 5050d setup-token` on the control host for a new one.")
		case exp < c.Now():
			return fail.Unauthorized("This setup link has expired. Run `sudo 5050d setup-token` on the control host for a new one.")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE setup_tokens SET used_at = ? WHERE token_hash = ?`, c.Now(), h); err != nil {
			return err
		}
		u, err = c.insertUser(ctx, tx, email, trim(req.Name), hash, api.RoleOwner)
		if err != nil {
			return err
		}
		s, err := c.installRow(ctx, tx)
		if err != nil {
			return err
		}
		s.SetupDone = true
		return c.putSetting(ctx, tx, "install", s, nil)
	})
	if err == nil {
		c.sendVerification(ctx, u)
	}
	return u, err
}

func (c *Core) insertUser(ctx context.Context, tx *sql.Tx, email, name, hash string, role api.Role) (api.User, error) {
	if name == "" {
		name = strings.SplitN(email, "@", 2)[0]
	}
	if len(name) > 64 {
		return api.User{}, fail.Validation("name", "The name must be at most 64 characters.")
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE email = ?`, email).Scan(&n); err != nil {
		return api.User{}, err
	}
	if n > 0 {
		return api.User{}, fail.Conflict("A user with the email %s already exists. Log in instead.", email)
	}
	u := api.User{ID: ids.New(ids.User), Email: email, Name: name, Role: role, CreatedAt: c.Now()}
	_, err := tx.ExecContext(ctx, `INSERT INTO users (id, email, name, password_hash, role, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.Name, hash, string(role), u.CreatedAt)
	return u, err
}

// ---------------------------------------------------------------- sessions

// NewSession stores a session and returns the cookie value.
func (c *Core) NewSession(ctx context.Context, userID, ip, userAgent string) (string, error) {
	v := auth.NewToken(32)
	now := c.Now()
	if len(userAgent) > 256 {
		userAgent = userAgent[:256]
	}
	err := c.st.Exec(ctx, `INSERT INTO sessions (id_hash, user_id, created_at, expires_at, extended_at, ip, user_agent) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		auth.HashToken(v), userID, now, now+SessionTTL, now, ip, userAgent)
	return v, err
}

// SessionActor authenticates a session cookie and slides its expiry.
func (c *Core) SessionActor(ctx context.Context, cookie string) (Actor, error) {
	h := auth.HashToken(cookie)
	var uid string
	var exp, ext, created int64
	err := c.st.Reader.QueryRowContext(ctx, `SELECT user_id, expires_at, extended_at, created_at FROM sessions WHERE id_hash = ?`, h).Scan(&uid, &exp, &ext, &created)
	if store.NoRows(err) || err == nil && (exp < c.Now() || c.Now()-created > sessionMaxAge) {
		return Actor{}, fail.Unauthorized("Your session has ended. Log in again.")
	}
	if err != nil {
		return Actor{}, err
	}
	u, err := c.userByID(ctx, c.st.Reader, uid)
	if err != nil {
		return Actor{}, fail.Unauthorized("Your session has ended. Log in again.")
	}
	if u.Disabled {
		return Actor{}, fail.Unauthorized("Your account is disabled. Ask an admin to enable it.")
	}
	if now := c.Now(); now-ext > sessionExtend && !c.st.ReadOnly() {
		_ = c.st.Exec(ctx, `UPDATE sessions SET expires_at = ?, extended_at = ? WHERE id_hash = ?`, now+SessionTTL, now, h)
	}
	return Actor{UserID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role, SessionHash: h}, nil
}

// EndSession deletes a session.
func (c *Core) EndSession(ctx context.Context, sessionHash string) error {
	return c.st.Exec(ctx, `DELETE FROM sessions WHERE id_hash = ?`, sessionHash)
}

// TokenActor authenticates an API token.
func (c *Core) TokenActor(ctx context.Context, secret string) (Actor, error) {
	if !auth.LooksLikeAPIToken(secret) {
		return Actor{}, fail.Unauthorized("The API token is not valid. Create a new one in Account > Tokens or run `5050 login`.")
	}
	var id, uid, name, scope, env string
	var exp, last int64
	err := c.st.Reader.QueryRowContext(ctx, `SELECT id, user_id, name, scope, environment_id, expires_at, last_used FROM api_tokens WHERE secret_hash = ?`,
		auth.HashToken(secret)).Scan(&id, &uid, &name, &scope, &env, &exp, &last)
	if store.NoRows(err) {
		return Actor{}, fail.Unauthorized("The API token is not valid or was deleted. Create a new one in Account > Tokens or run `5050 login`.")
	}
	if err != nil {
		return Actor{}, err
	}
	if exp > 0 && exp < c.Now() {
		return Actor{}, fail.Unauthorized("The API token %q has expired. Create a new one in Account > Tokens.", name)
	}
	u, err := c.userByID(ctx, c.st.Reader, uid)
	if err != nil || u.Disabled {
		return Actor{}, fail.Unauthorized("The API token belongs to a user who no longer has access.")
	}
	if now := c.Now(); now-last > minute && !c.st.ReadOnly() {
		_ = c.st.Exec(ctx, `UPDATE api_tokens SET last_used = ? WHERE id = ?`, now, id)
	}
	return Actor{UserID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role, TokenName: name, TokenScope: scope, TokenEnv: env}, nil
}

// ---------------------------------------------------------------- login

type ticket struct {
	userID string
	exp    time.Time
}

type ticketTable struct {
	mu sync.Mutex
	m  map[string]ticket
}

var tickets = &ticketTable{m: map[string]ticket{}}

func (t *ticketTable) put(userID string, now time.Time) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, v := range t.m {
		if now.After(v.exp) {
			delete(t.m, k)
		}
	}
	k := auth.NewToken(24)
	t.m[k] = ticket{userID: userID, exp: now.Add(TicketTTL)}
	return k
}

func (t *ticketTable) take(k string, now time.Time) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v, ok := t.m[k]
	delete(t.m, k) // single use; keep puts it back after a wrong code
	if !ok || now.After(v.exp) {
		return "", false
	}
	return v.userID, true
}

// keep puts a ticket back after a wrong code, so the person can retry.
func (t *ticketTable) keep(k, userID string, exp time.Time) {
	t.mu.Lock()
	t.m[k] = ticket{userID: userID, exp: exp}
	t.mu.Unlock()
}

// Login checks a password. It returns the user, or a ticket when a TOTP
// code is needed next. Rate limits are applied by the API layer.
func (c *Core) Login(ctx context.Context, email, password string) (u api.User, ticket string, err error) {
	row, ok, err := c.userByEmail(ctx, c.st.Reader, email)
	if err != nil {
		return api.User{}, "", err
	}
	if !ok {
		if err := auth.DummyCheck(password); err != nil {
			return api.User{}, "", err
		}
		return api.User{}, "", fail.Unauthorized("The email or password is not correct.")
	}
	good, err := auth.VerifyPassword(row.PasswordHash, password)
	if err != nil {
		return api.User{}, "", err
	}
	if !good {
		return api.User{}, "", fail.Unauthorized("The email or password is not correct.")
	}
	if row.Disabled {
		return api.User{}, "", fail.Forbidden("Your account is disabled. Ask an admin to enable it.")
	}
	if row.TOTPEnabled {
		return api.User{}, tickets.put(row.ID, c.now()), nil
	}
	return row.User, "", nil
}

// LoginTOTP finishes a login with a TOTP or recovery code.
func (c *Core) LoginTOTP(ctx context.Context, tk, code string) (api.User, error) {
	now := c.now()
	uid, ok := tickets.take(tk, now)
	if !ok {
		return api.User{}, fail.Unauthorized("The login took too long. Enter your email and password again.")
	}
	ok, err := c.checkSecondFactor(ctx, uid, code)
	if err != nil {
		return api.User{}, err
	}
	if !ok {
		tickets.keep(tk, uid, now.Add(TicketTTL))
		return api.User{}, fail.Unauthorized("The code is not correct. Enter the 6-digit code from your authenticator app or a recovery code.")
	}
	return c.User(ctx, uid)
}

// totpSteps remembers each user's last accepted time step, so a code cannot be
// used twice inside its 90 s window. shortcut: in memory, a restart forgets it,
// which is fine for a window this short; one control host runs one process.
var totpSteps = &totpStepTable{m: map[string]int64{}}

type totpStepTable struct {
	mu sync.Mutex
	m  map[string]int64
}

func (t *totpStepTable) use(userID string, step int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if step <= t.m[userID] {
		return false
	}
	t.m[userID] = step
	return true
}

// checkSecondFactor validates a TOTP code or consumes a recovery code.
func (c *Core) checkSecondFactor(ctx context.Context, userID, code string) (bool, error) {
	row, err := c.userByID(ctx, c.st.Reader, userID)
	if err != nil {
		return false, err
	}
	if !row.TOTPEnabled {
		return true, nil
	}
	secret, err := c.box.Open(row.TOTPSecretEnc, totpAAD(userID))
	if err != nil {
		return false, fail.Internal(err)
	}
	if step, ok := auth.TOTPStep(string(secret), code, c.now()); ok {
		return totpSteps.use(userID, step), nil
	}
	rest, used := auth.UseRecoveryCode(row.RecoveryHash, code)
	if !used {
		return false, nil
	}
	return true, c.st.Exec(ctx, `UPDATE users SET recovery_codes_hash = ? WHERE id = ?`, jsonString(rest), userID)
}

// passwordOK is for signed-in re-checks. When the server is flooded, a right
// password can read as wrong here; the person retries.
func (c *Core) passwordOK(hash, pw string) bool {
	ok, err := auth.VerifyPassword(hash, pw)
	return err == nil && ok
}

// ---------------------------------------------------------------- account

// UpdateMe is PATCH /me.
func (c *Core) UpdateMe(ctx context.Context, a Actor, req api.UpdateMeRequest) (api.User, error) {
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		row, err := c.userByID(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		if req.Name != nil {
			n := trim(*req.Name)
			if n == "" || len(n) > 64 {
				return fail.Validation("name", "The name must be 1 to 64 characters.")
			}
			if _, err := tx.ExecContext(ctx, `UPDATE users SET name = ? WHERE id = ?`, n, a.UserID); err != nil {
				return err
			}
		}
		if req.Password != nil {
			if req.CurrentPassword == nil || !c.passwordOK(row.PasswordHash, *req.CurrentPassword) {
				return fail.Validation("currentPassword", "Your current password is not correct.")
			}
			if err := auth.CheckPasswordPolicy("password", *req.Password); err != nil {
				return err
			}
			h, err := auth.HashPassword(*req.Password)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, h, a.UserID); err != nil {
				return err
			}
			// Other sessions end; this one stays.
			if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND id_hash != ?`, a.UserID, a.SessionHash); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return api.User{}, err
	}
	return c.User(ctx, a.UserID)
}

// TOTPSetup starts 2FA setup: a new secret, not active until confirmed.
func (c *Core) TOTPSetup(ctx context.Context, a Actor) (api.TOTPSetupResponse, error) {
	row, err := c.userByID(ctx, c.st.Reader, a.UserID)
	if err != nil {
		return api.TOTPSetupResponse{}, err
	}
	if row.TOTPEnabled {
		return api.TOTPSetupResponse{}, fail.Conflict("Two-factor authentication is already on. Turn it off first to set up a new device.")
	}
	in, _ := c.installRow(ctx, c.st.Reader)
	secret, url, err := auth.NewTOTP("5050cloud "+in.Name, row.Email)
	if err != nil {
		return api.TOTPSetupResponse{}, err
	}
	enc, err := c.box.Seal([]byte(secret), totpAAD(a.UserID))
	if err != nil {
		return api.TOTPSetupResponse{}, err
	}
	if err := c.st.Exec(ctx, `UPDATE users SET totp_secret_enc = ?, totp_enabled = 0 WHERE id = ?`, enc, a.UserID); err != nil {
		return api.TOTPSetupResponse{}, err
	}
	return api.TOTPSetupResponse{Secret: secret, OTPAuthURL: url}, nil
}

// TOTPEnable confirms setup with a code and returns recovery codes.
func (c *Core) TOTPEnable(ctx context.Context, a Actor, code string) (api.TOTPEnableResponse, error) {
	row, err := c.userByID(ctx, c.st.Reader, a.UserID)
	if err != nil {
		return api.TOTPEnableResponse{}, err
	}
	if row.TOTPEnabled {
		return api.TOTPEnableResponse{}, fail.Conflict("Two-factor authentication is already on.")
	}
	if len(row.TOTPSecretEnc) == 0 {
		return api.TOTPEnableResponse{}, fail.Conflict("Start the setup first: open Account > Security and scan the QR code.")
	}
	secret, err := c.box.Open(row.TOTPSecretEnc, totpAAD(a.UserID))
	if err != nil {
		return api.TOTPEnableResponse{}, fail.Internal(err)
	}
	if !auth.ValidateTOTP(string(secret), code, c.now()) {
		return api.TOTPEnableResponse{}, fail.Validation("code", "The code is not correct. Check the time on your phone and enter the current 6-digit code.")
	}
	codes, hashes := auth.NewRecoveryCodes()
	err = c.st.Exec(ctx, `UPDATE users SET totp_enabled = 1, recovery_codes_hash = ? WHERE id = ?`, jsonString(hashes), a.UserID)
	return api.TOTPEnableResponse{RecoveryCodes: codes}, err
}

// TOTPDisable turns 2FA off after a password and code check.
func (c *Core) TOTPDisable(ctx context.Context, a Actor, password, code string) error {
	row, err := c.userByID(ctx, c.st.Reader, a.UserID)
	if err != nil {
		return err
	}
	if !c.passwordOK(row.PasswordHash, password) {
		return fail.Validation("password", "Your password is not correct.")
	}
	if row.TOTPEnabled {
		ok, err := c.checkSecondFactor(ctx, a.UserID, code)
		if err != nil {
			return err
		}
		if !ok {
			return fail.Validation("code", "The code is not correct.")
		}
	}
	return c.st.Exec(ctx, `UPDATE users SET totp_enabled = 0, totp_secret_enc = NULL, recovery_codes_hash = '[]' WHERE id = ?`, a.UserID)
}

// Reset2FA removes 2FA for an email (`5050d reset-2fa`).
func (c *Core) Reset2FA(ctx context.Context, email string) error {
	row, ok, err := c.userByEmail(ctx, c.st.Reader, email)
	if err != nil {
		return err
	}
	if !ok {
		return fail.NotFound("No user has the email %s.", normEmail(email))
	}
	return c.st.Exec(ctx, `UPDATE users SET totp_enabled = 0, totp_secret_enc = NULL, recovery_codes_hash = '[]' WHERE id = ?`, row.ID)
}

// ---------------------------------------------------------------- password reset and verification

// Forgot sends a reset link when SMTP is set. It never reveals whether the email exists.
func (c *Core) Forgot(ctx context.Context, email string) error {
	row, ok, err := c.userByEmail(ctx, c.st.Reader, email)
	if err != nil || !ok || row.Disabled {
		return err
	}
	s, err := c.smtpSettings(ctx, c.st.Reader)
	if err != nil || s.Host == "" {
		return err
	}
	link, err := c.newResetLink(ctx, row.ID)
	if err != nil {
		return err
	}
	in, _ := c.installRow(ctx, c.st.Reader)
	subj, text := mail.ResetPassword(in.Name, link)
	if err := c.mailer(s).Send(ctx, mail.Message{To: []string{row.Email}, Subject: subj, Text: text}); err != nil {
		c.log.Warn("cannot send a password reset email", "err", err)
	}
	return nil
}

func (c *Core) newResetLink(ctx context.Context, userID string) (string, error) {
	t := auth.NewToken(32)
	err := c.st.Exec(ctx, `INSERT INTO password_resets (token_hash, user_id, expires_at) VALUES (?, ?, ?)`, auth.HashToken(t), userID, c.Now()+ResetTTL)
	return c.ConsoleURL(ctx) + "/reset?token=" + t, err
}

// ResetLink prints a one-time reset link for `5050d reset-password`.
func (c *Core) ResetLink(ctx context.Context, email string) (string, error) {
	row, ok, err := c.userByEmail(ctx, c.st.Reader, email)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fail.NotFound("No user has the email %s.", normEmail(email))
	}
	return c.newResetLink(ctx, row.ID)
}

// ResetPassword sets a new password with a reset token and ends every session of the user.
// It returns a TOTP ticket instead of letting the caller sign the user in when
// the account has a second factor: a mailbox alone must not be enough.
func (c *Core) ResetPassword(ctx context.Context, token, password string) (u api.User, ticket string, err error) {
	if err := auth.CheckPasswordPolicy("password", password); err != nil {
		return api.User{}, "", err
	}
	// Check the token before the 64 MiB hash, so an anonymous caller cannot make one.
	var exp int64
	var used sql.NullInt64
	var known string
	err = c.st.Reader.QueryRowContext(ctx, `SELECT user_id, expires_at, used_at FROM password_resets WHERE token_hash = ?`, auth.HashToken(trim(token))).Scan(&known, &exp, &used)
	if store.NoRows(err) || err == nil && (used.Valid || exp < c.Now()) {
		return api.User{}, "", fail.Unauthorized("This reset link is not valid or has expired. Ask for a new one.")
	}
	if err != nil {
		return api.User{}, "", err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return api.User{}, "", err
	}
	var uid string
	err = c.st.Write(ctx, func(tx *sql.Tx) error {
		var exp int64
		var used sql.NullInt64
		h := auth.HashToken(trim(token))
		err := tx.QueryRowContext(ctx, `SELECT user_id, expires_at, used_at FROM password_resets WHERE token_hash = ?`, h).Scan(&uid, &exp, &used)
		if store.NoRows(err) || err == nil && (used.Valid || exp < c.Now()) {
			return fail.Unauthorized("This reset link is not valid or has expired. Ask for a new one.")
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE password_resets SET used_at = ? WHERE user_id = ? AND used_at IS NULL`, c.Now(), uid); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, uid); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, uid)
		return err
	})
	if err != nil {
		return api.User{}, "", err
	}
	row, err := c.userByID(ctx, c.st.Reader, uid)
	if err != nil {
		return api.User{}, "", err
	}
	if row.Disabled {
		return api.User{}, "", fail.Forbidden("Your account is disabled. Ask an admin to enable it.")
	}
	if row.TOTPEnabled {
		return api.User{}, tickets.put(row.ID, c.now()), nil
	}
	return row.User, "", nil
}

// sendVerification emails a verification link when SMTP is set.
func (c *Core) sendVerification(ctx context.Context, u api.User) {
	s, err := c.smtpSettings(ctx, c.st.Reader)
	if err != nil || s.Host == "" {
		return
	}
	t := auth.NewToken(32)
	if err := c.st.Exec(ctx, `INSERT INTO email_verifications (token_hash, user_id, expires_at) VALUES (?, ?, ?)`, auth.HashToken(t), u.ID, c.Now()+VerifyTTL); err != nil {
		return
	}
	in, _ := c.installRow(ctx, c.st.Reader)
	subj, text := mail.Verify(in.Name, c.ConsoleURL(ctx)+"/verify-email?token="+t)
	if err := c.mailer(s).Send(ctx, mail.Message{To: []string{u.Email}, Subject: subj, Text: text}); err != nil {
		c.log.Warn("cannot send a verification email", "err", err)
	}
}

// VerifyEmail marks an address verified.
func (c *Core) VerifyEmail(ctx context.Context, token string) error {
	return c.st.Write(ctx, func(tx *sql.Tx) error {
		var uid string
		var exp int64
		var used sql.NullInt64
		h := auth.HashToken(trim(token))
		err := tx.QueryRowContext(ctx, `SELECT user_id, expires_at, used_at FROM email_verifications WHERE token_hash = ?`, h).Scan(&uid, &exp, &used)
		if store.NoRows(err) || err == nil && (used.Valid || exp < c.Now()) {
			return fail.Unauthorized("This verification link is not valid or has expired.")
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE email_verifications SET used_at = ? WHERE token_hash = ?`, c.Now(), h); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE users SET email_verified_at = ? WHERE id = ?`, c.Now(), uid)
		return err
	})
}

// ---------------------------------------------------------------- API tokens

// Tokens lists the actor's tokens.
func (c *Core) Tokens(ctx context.Context, a Actor) ([]api.APIToken, error) {
	return store.All(ctx, c.st.Reader, func(r *sql.Rows) (api.APIToken, error) {
		var t api.APIToken
		return t, r.Scan(&t.ID, &t.Name, &t.Prefix, &t.CreatedAt, &t.LastUsed, &t.ExpiresAt, &t.Scope, &t.EnvironmentID)
	}, `SELECT id, name, prefix, created_at, last_used, expires_at, scope, environment_id FROM api_tokens WHERE user_id = ? ORDER BY id DESC`, a.UserID)
}

// CreateToken makes an API token; the secret is returned once.
func (c *Core) CreateToken(ctx context.Context, a Actor, req api.CreateTokenRequest) (api.CreateTokenResponse, error) {
	name := trim(req.Name)
	if name == "" || len(name) > 64 {
		return api.CreateTokenResponse{}, fail.Validation("name", "Give the token a name of 1 to 64 characters.")
	}
	scope := req.Scope
	if scope == "" {
		scope = "account"
	}
	switch scope {
	case "account":
		if req.EnvironmentID != "" {
			return api.CreateTokenResponse{}, fail.Validation("environmentId", "Account tokens act as you everywhere; leave the environment empty or choose the environment scope.")
		}
		if a.TokenScope == "environment" {
			return api.CreateTokenResponse{}, fail.Forbidden("An environment token cannot create account tokens.")
		}
	case "environment":
		env, prj, err := c.Environment(ctx, req.EnvironmentID)
		if err != nil {
			return api.CreateTokenResponse{}, fail.Validation("environmentId", "Choose an environment for an environment token.")
		}
		if lvl, _ := c.ProjectLevel(ctx, a, prj.ID); !atLeast(lvl, Member) {
			return api.CreateTokenResponse{}, fail.Forbidden("You need editor access to project %s to create a token for environment %s.", prj.Name, env.Name)
		}
		if a.TokenScope == "environment" && a.TokenEnv != env.ID {
			return api.CreateTokenResponse{}, fail.Forbidden("An environment token can only create tokens for its own environment.")
		}
	default:
		return api.CreateTokenResponse{}, fail.Validation("scope", "The scope must be \"account\" or \"environment\".")
	}
	if req.ExpiresInDays < 0 || req.ExpiresInDays > 3650 {
		return api.CreateTokenResponse{}, fail.Validation("expiresInDays", "The expiry must be between 0 (never) and 3650 days.")
	}
	secret, prefix := auth.NewAPIToken()
	t := api.APIToken{ID: ids.New(ids.Token), Name: name, Prefix: prefix, CreatedAt: c.Now(), Scope: scope, EnvironmentID: req.EnvironmentID}
	if req.ExpiresInDays > 0 {
		t.ExpiresAt = t.CreatedAt + int64(req.ExpiresInDays)*day
	}
	err := c.st.Exec(ctx, `INSERT INTO api_tokens (id, user_id, name, prefix, secret_hash, scope, environment_id, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, a.UserID, t.Name, t.Prefix, auth.HashToken(secret), t.Scope, t.EnvironmentID, t.CreatedAt, t.ExpiresAt)
	return api.CreateTokenResponse{Token: t, Secret: secret}, err
}

// DeleteToken deletes one of the actor's tokens.
func (c *Core) DeleteToken(ctx context.Context, a Actor, id string) error {
	return c.st.Write(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, a.UserID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fail.NotFound("You have no token with the id %s.", id)
		}
		return nil
	})
}

// ---------------------------------------------------------------- CLI login

// CLIAuthStart begins a browser login for the CLI.
func (c *Core) CLIAuthStart(ctx context.Context) (api.CLIAuthStart, error) {
	code := auth.NewCLICode()
	poll := auth.NewToken(32)
	exp := c.now().Add(CLIAuthTTL).UnixMilli()
	err := c.st.Exec(ctx, `INSERT INTO cli_auth (code, poll_hash, status, expires_at, created_at) VALUES (?, ?, 'pending', ?, ?)`,
		code, auth.HashToken(poll), exp, c.Now())
	return api.CLIAuthStart{Code: code, VerifyURL: c.ConsoleURL(ctx) + "/cli-auth?code=" + code, PollToken: poll, ExpiresAt: exp, Interval: 2}, err
}

// CLIAuthApprove approves a code for the signed-in user.
func (c *Core) CLIAuthApprove(ctx context.Context, a Actor, code string) error {
	if a.TokenName != "" {
		return fail.Forbidden("Approve a CLI login from the console in your browser, not with an API token.")
	}
	return c.st.Write(ctx, func(tx *sql.Tx) error {
		var status string
		var exp int64
		err := tx.QueryRowContext(ctx, `SELECT status, expires_at FROM cli_auth WHERE code = ?`, auth.NormalizeCode(code)).Scan(&status, &exp)
		if store.NoRows(err) {
			return fail.NotFound("The code %s is not valid. Run `5050 login` again.", auth.NormalizeCode(code))
		}
		if err != nil {
			return err
		}
		if exp < c.Now() {
			return fail.Conflict("The code has expired. Run `5050 login` again.")
		}
		if status != "pending" {
			return fail.Conflict("This code was already used.")
		}
		_, err = tx.ExecContext(ctx, `UPDATE cli_auth SET status = 'approved', user_id = ? WHERE code = ?`, a.UserID, auth.NormalizeCode(code))
		return err
	})
}

// CLIAuthPoll returns the state of a CLI login; once approved it mints the
// token exactly once.
func (c *Core) CLIAuthPoll(ctx context.Context, pollToken string) (api.CLIAuthPollResponse, error) {
	var out api.CLIAuthPollResponse
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		var code, status string
		var uid sql.NullString
		var exp int64
		h := auth.HashToken(trim(pollToken))
		err := tx.QueryRowContext(ctx, `SELECT code, status, user_id, expires_at FROM cli_auth WHERE poll_hash = ?`, h).Scan(&code, &status, &uid, &exp)
		if store.NoRows(err) {
			out.Status = "expired"
			return nil
		}
		if err != nil {
			return err
		}
		switch {
		case status == "pending" && exp < c.Now():
			out.Status = "expired"
		case status == "pending":
			out.Status = "pending"
		case status == "approved" && uid.Valid:
			secret, prefix := auth.NewAPIToken()
			id := ids.New(ids.Token)
			name := "CLI login " + c.now().UTC().Format("2006-01-02 15:04")
			if _, err := tx.ExecContext(ctx, `INSERT INTO api_tokens (id, user_id, name, prefix, secret_hash, scope, created_at) VALUES (?, ?, ?, ?, ?, 'account', ?)`,
				id, uid.String, name, prefix, auth.HashToken(secret), c.Now()); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM cli_auth WHERE code = ?`, code); err != nil {
				return err
			}
			u, err := c.userByID(ctx, tx, uid.String)
			if err != nil {
				return err
			}
			out = api.CLIAuthPollResponse{Status: "approved", Token: secret, User: &u.User}
		default:
			out.Status = "denied"
		}
		return nil
	})
	return out, err
}

// ---------------------------------------------------------------- members and invites

// Members lists users.
func (c *Core) Members(ctx context.Context) ([]api.User, error) {
	rows, err := store.All(ctx, c.st.Reader, func(r *sql.Rows) (userRow, error) { return scanUser(r) },
		`SELECT `+userCols+` FROM users ORDER BY created_at`)
	out := make([]api.User, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.User)
	}
	return out, err
}

func validInviteRole(r api.Role) bool {
	return r == api.RoleAdmin || r == api.RoleMember || r == api.RoleViewer
}

// CreateInvite makes an invite link, emailing it when SMTP is set.
func (c *Core) CreateInvite(ctx context.Context, a Actor, req api.CreateInviteRequest) (api.Invite, error) {
	if !validInviteRole(req.Role) {
		return api.Invite{}, fail.Validation("role", "The role must be admin, member or viewer. Ownership moves with Transfer Ownership.")
	}
	email := normEmail(req.Email)
	if email != "" && !validEmail(email) {
		return api.Invite{}, fail.Validation("email", "Enter a valid email address, or leave it empty for a link invite.")
	}
	if req.ProjectID != "" {
		if _, err := c.Project(ctx, req.ProjectID); err != nil {
			return api.Invite{}, fail.Validation("projectId", "The project %s does not exist.", req.ProjectID)
		}
	}
	maxUses := req.MaxUses
	if maxUses == 0 {
		maxUses = 1
	}
	if maxUses < 1 || maxUses > 1000 {
		return api.Invite{}, fail.Validation("maxUses", "The use limit must be between 1 and 1000.")
	}
	hours := req.ExpiresInHours
	if hours == 0 {
		hours = InviteDefaultH
	}
	if hours < 1 || hours > 24*90 {
		return api.Invite{}, fail.Validation("expiresInHours", "An invite can be valid for 1 hour up to 90 days.")
	}
	if email != "" {
		if _, ok, _ := c.userByEmail(ctx, c.st.Reader, email); ok {
			return api.Invite{}, fail.Conflict("%s is already a member of this install.", email)
		}
	}
	tok := auth.NewToken(24)
	inv := api.Invite{ID: ids.New(ids.Invite), Role: req.Role, Email: email, ProjectID: req.ProjectID, MaxUses: maxUses,
		ExpiresAt: c.Now() + int64(hours)*hour, CreatedAt: c.Now()}
	inv.URL = c.ConsoleURL(ctx) + "/invite/" + tok
	err := c.st.Exec(ctx, `INSERT INTO invites (id, role, project_id, email, token_hash, max_uses, expires_at, created_at, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inv.ID, string(inv.Role), nullStr(inv.ProjectID), nullStr(email), auth.HashToken(tok), inv.MaxUses, inv.ExpiresAt, inv.CreatedAt, a.UserID)
	if err != nil {
		return api.Invite{}, err
	}
	if email != "" {
		if s, err := c.smtpSettings(ctx, c.st.Reader); err == nil && s.Host != "" {
			in, _ := c.installRow(ctx, c.st.Reader)
			subj, text := mail.Invite(in.Name, a.Label(), string(inv.Role), inv.URL, time.UnixMilli(inv.ExpiresAt))
			if err := c.mailer(s).Send(ctx, mail.Message{To: []string{email}, Subject: subj, Text: text}); err != nil {
				c.log.Warn("cannot send an invite email", "err", err)
			}
		}
	}
	return inv, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Invites lists open invites.
func (c *Core) Invites(ctx context.Context) ([]api.Invite, error) {
	return store.All(ctx, c.st.Reader, func(r *sql.Rows) (api.Invite, error) {
		var i api.Invite
		var pid, email sql.NullString
		err := r.Scan(&i.ID, &i.Role, &pid, &email, &i.MaxUses, &i.Uses, &i.ExpiresAt, &i.CreatedAt)
		i.ProjectID, i.Email = pid.String, email.String
		return i, err
	}, `SELECT id, role, project_id, email, max_uses, uses, expires_at, created_at FROM invites WHERE uses < max_uses AND expires_at > ? ORDER BY id DESC`, c.Now())
}

// DeleteInvite revokes an invite.
func (c *Core) DeleteInvite(ctx context.Context, id string) error {
	return c.st.Write(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM invites WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fail.NotFound("The invite %s does not exist.", id)
		}
		return nil
	})
}

type inviteRow struct {
	id, role, projectID, email string
	maxUses, uses              int
	expiresAt                  int64
}

func (c *Core) inviteByToken(ctx context.Context, q store.Querier, tok string) (inviteRow, bool, error) {
	var r inviteRow
	var pid, email sql.NullString
	err := q.QueryRowContext(ctx, `SELECT id, role, project_id, email, max_uses, uses, expires_at FROM invites WHERE token_hash = ?`,
		auth.HashToken(trim(tok))).Scan(&r.id, &r.role, &pid, &email, &r.maxUses, &r.uses, &r.expiresAt)
	if store.NoRows(err) {
		return r, false, nil
	}
	r.projectID, r.email = pid.String, email.String
	return r, err == nil, err
}

func (c *Core) inviteProblem(r inviteRow) string {
	switch {
	case r.expiresAt < c.Now():
		return "This invite has expired. Ask an admin for a new one."
	case r.uses >= r.maxUses:
		return "This invite was already used. Ask an admin for a new one."
	}
	return ""
}

// InvitePreview is the public accept page.
func (c *Core) InvitePreview(ctx context.Context, tok string) (api.InvitePreview, error) {
	in, _ := c.installRow(ctx, c.st.Reader)
	r, ok, err := c.inviteByToken(ctx, c.st.Reader, tok)
	if err != nil {
		return api.InvitePreview{}, err
	}
	if !ok {
		return api.InvitePreview{Valid: false, Reason: "This invite link is not valid.", InstallName: in.Name}, nil
	}
	p := api.InvitePreview{Valid: true, Role: api.Role(r.role), InstallName: in.Name, Email: r.email, ExpiresAt: r.expiresAt}
	if why := c.inviteProblem(r); why != "" {
		p.Valid, p.Reason = false, why
	}
	if r.projectID != "" {
		if prj, err := c.Project(ctx, r.projectID); err == nil {
			p.ProjectName = prj.Name
		}
	}
	return p, nil
}

// AcceptInvite creates the user of an invite.
func (c *Core) AcceptInvite(ctx context.Context, req api.AcceptInviteRequest) (api.User, error) {
	email := normEmail(req.Email)
	if !validEmail(email) {
		return api.User{}, fail.Validation("email", "Enter a valid email address.")
	}
	if err := auth.CheckPasswordPolicy("password", req.Password); err != nil {
		return api.User{}, err
	}
	// Check the token before the 64 MiB hash, so an anonymous caller cannot make one.
	r, ok, err := c.inviteByToken(ctx, c.st.Reader, req.Token)
	if err != nil {
		return api.User{}, err
	}
	if !ok {
		return api.User{}, fail.Unauthorized("This invite link is not valid.")
	}
	if why := c.inviteProblem(r); why != "" {
		return api.User{}, fail.Unauthorized("%s", why)
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return api.User{}, err
	}
	var u api.User
	err = c.st.Write(ctx, func(tx *sql.Tx) error {
		r, ok, err := c.inviteByToken(ctx, tx, req.Token)
		if err != nil {
			return err
		}
		if !ok {
			return fail.Unauthorized("This invite link is not valid.")
		}
		if why := c.inviteProblem(r); why != "" {
			return fail.Unauthorized("%s", why)
		}
		if r.email != "" && r.email != email {
			return fail.Validation("email", "This invite is for %s. Sign up with that address.", r.email)
		}
		u, err = c.insertUser(ctx, tx, email, trim(req.Name), hash, api.Role(r.role))
		if err != nil {
			return err
		}
		if r.email != "" {
			// The link reached the invited mailbox, so the address is verified.
			if _, err := tx.ExecContext(ctx, `UPDATE users SET email_verified_at = ? WHERE id = ?`, c.Now(), u.ID); err != nil {
				return err
			}
			u.EmailVerified = true
		}
		if r.projectID != "" && api.Role(r.role) != api.RoleAdmin {
			if err := c.grantOnly(ctx, tx, u.ID, api.Role(r.role), r.projectID); err != nil {
				return err
			}
		}
		var accepted []string
		var raw string
		_ = tx.QueryRowContext(ctx, `SELECT accepted_by FROM invites WHERE id = ?`, r.id).Scan(&raw)
		_ = json.Unmarshal([]byte(raw), &accepted)
		accepted = append(accepted, u.ID)
		_, err = tx.ExecContext(ctx, `UPDATE invites SET uses = uses + 1, accepted_by = ? WHERE id = ?`, jsonString(accepted), r.id)
		return err
	})
	if err == nil && !u.EmailVerified {
		c.sendVerification(ctx, u)
	}
	return u, err
}

// grantOnly limits a project invitee to that project: a grant there and
// "none" on every other existing project.
func (c *Core) grantOnly(ctx context.Context, tx *sql.Tx, userID string, role api.Role, projectID string) error {
	g := api.ProjectEditor
	if role == api.RoleViewer {
		g = api.ProjectViewer
	}
	pids, err := store.Strings(ctx, tx, `SELECT id FROM projects WHERE deleted_at IS NULL`)
	if err != nil {
		return err
	}
	for _, pid := range pids {
		r := api.ProjectNone
		if pid == projectID {
			r = g
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO project_grants (project_id, user_id, role) VALUES (?, ?, ?)`, pid, userID, string(r)); err != nil {
			return err
		}
	}
	return nil
}

// UpdateMember changes a role or disables a user.
func (c *Core) UpdateMember(ctx context.Context, a Actor, id string, req api.UpdateMemberRequest) (api.User, error) {
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		row, err := c.userByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if row.Role == api.RoleOwner && a.Role != api.RoleOwner {
			return fail.Forbidden("Only the owner can change the owner's account.")
		}
		if req.Role != nil {
			if *req.Role == api.RoleOwner || row.Role == api.RoleOwner {
				return fail.Validation("role", "Ownership moves only with Transfer Ownership.")
			}
			if !validInviteRole(*req.Role) {
				return fail.Validation("role", "The role must be admin, member or viewer.")
			}
			if _, err := tx.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, string(*req.Role), id); err != nil {
				return err
			}
		}
		if req.Disabled != nil {
			if row.Role == api.RoleOwner {
				return fail.Validation("disabled", "The owner cannot be disabled.")
			}
			if id == a.UserID {
				return fail.Validation("disabled", "You cannot disable your own account.")
			}
			if _, err := tx.ExecContext(ctx, `UPDATE users SET disabled = ? WHERE id = ?`, store.Bool(*req.Disabled), id); err != nil {
				return err
			}
			if *req.Disabled {
				if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return api.User{}, err
	}
	return c.User(ctx, id)
}

// DeleteMember removes a user, their sessions, tokens and grants.
func (c *Core) DeleteMember(ctx context.Context, a Actor, id string) error {
	return c.st.Write(ctx, func(tx *sql.Tx) error {
		row, err := c.userByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if row.Role == api.RoleOwner {
			return fail.Forbidden("The owner cannot be removed. Transfer ownership first.")
		}
		if id == a.UserID {
			return fail.Validation("id", "You cannot remove yourself. Ask another admin.")
		}
		for _, q := range []string{
			`DELETE FROM sessions WHERE user_id = ?`, `DELETE FROM api_tokens WHERE user_id = ?`,
			`DELETE FROM project_grants WHERE user_id = ?`, `DELETE FROM notification_prefs WHERE user_id = ?`,
			`DELETE FROM notifications WHERE user_id = ?`, `DELETE FROM users WHERE id = ?`,
		} {
			if _, err := tx.ExecContext(ctx, q, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// TransferOwnership makes an admin the owner after a password (and 2FA) check.
func (c *Core) TransferOwnership(ctx context.Context, a Actor, toID string, req api.TransferOwnershipRequest) error {
	me, err := c.userByID(ctx, c.st.Reader, a.UserID)
	if err != nil {
		return err
	}
	if me.Role != api.RoleOwner {
		return fail.Forbidden("Only the owner can transfer ownership.")
	}
	if !c.passwordOK(me.PasswordHash, req.Password) {
		return fail.Validation("password", "Your password is not correct.")
	}
	if me.TOTPEnabled {
		ok, err := c.checkSecondFactor(ctx, me.ID, req.Code)
		if err != nil {
			return err
		}
		if !ok {
			return fail.Validation("code", "The two-factor code is not correct.")
		}
	}
	return c.st.Write(ctx, func(tx *sql.Tx) error {
		to, err := c.userByID(ctx, tx, toID)
		if err != nil {
			return err
		}
		if to.Role != api.RoleAdmin || to.Disabled {
			return fail.Validation("id", "Ownership can only go to an active admin. Make %s an admin first.", to.Email)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET role = 'owner' WHERE id = ?`, to.ID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE users SET role = 'admin' WHERE id = ?`, me.ID)
		return err
	})
}
