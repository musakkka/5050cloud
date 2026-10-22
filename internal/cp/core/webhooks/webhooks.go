// SPDX-License-Identifier: AGPL-3.0-only

// Package webhooks delivers signed JSON events to project webhooks and the
// install alert webhook (docs/SPEC.md 9.15). Deliveries are rows in
// webhook_deliveries, so they survive a restart: a worker sends due rows with
// a 10 second timeout, retries a failure 3 times with backoff, and records
// the response code. Every connection goes through the SSRF guard in
// guard.go.
package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/ids"
	"github.com/musakkka/5050cloud/internal/cp/core/store"
	"github.com/musakkka/5050cloud/internal/cp/secretbox"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/version"
)

// Timeout is the per-attempt deadline.
const Timeout = 10 * time.Second

// Retries is how many times a failed delivery is retried.
const Retries = 3

// Backoff is the wait before retry n (1-based).
var Backoff = []time.Duration{10 * time.Second, time.Minute, 5 * time.Minute}

// Critical is the preset "critical" (9.15).
var Critical = []string{
	api.HookDeploymentCrashed, api.HookReplicaOOM, api.HookVolumeHigh,
	api.HookDiskHigh, api.HookBackupFailed, api.HookMonitorTriggered,
}

// AllEvents lists every event a project webhook can subscribe to.
var AllEvents = []string{
	api.HookDeploymentSuccess, api.HookDeploymentFailed, api.HookDeploymentCrashed,
	api.HookReplicaOOM, api.HookServiceUnhealthy, api.HookVolumeHigh, api.HookDiskHigh,
	api.HookBackupFailed, api.HookMonitorTriggered, api.HookMonitorResolved, api.HookHostOffline,
}

// Body is the JSON every delivery carries.
type Body struct {
	Type        string `json:"type"`
	Timestamp   int64  `json:"timestamp"`
	Project     string `json:"project,omitempty"`
	Environment string `json:"environment,omitempty"`
	Service     string `json:"service,omitempty"`
	Deployment  string `json:"deployment,omitempty"`
	Details     any    `json:"details,omitempty"`
}

// Sign returns the X-5050-Signature value for body.
func Sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// Verify checks a signature header (used by tests and documented for receivers).
func Verify(secret string, body []byte, header string) bool {
	return hmac.Equal([]byte(Sign(secret, body)), []byte(header))
}

// Subscribed reports whether a webhook with events and preset wants event.
func Subscribed(events []string, preset, event string) bool {
	if preset == "critical" {
		for _, e := range Critical {
			if e == event {
				return true
			}
		}
	}
	for _, e := range events {
		if e == event || e == "*" {
			return true
		}
	}
	return false
}

// Dispatcher queues and sends deliveries.
type Dispatcher struct {
	st     *store.Store
	box    *secretbox.Box
	client func() *http.Client
	now    func() time.Time
	wake   chan struct{}
	log    *slog.Logger
	// AlertSecret signs install alert deliveries (they have no per-hook secret).
	AlertSecret func() string
}

// New returns a dispatcher. client is called per send so a settings change
// (allow private destinations) takes effect at once.
func New(st *store.Store, box *secretbox.Box, client func() *http.Client, log *slog.Logger) *Dispatcher {
	if log == nil {
		log = slog.Default()
	}
	return &Dispatcher{st: st, box: box, client: client, now: time.Now, wake: make(chan struct{}, 1), log: log}
}

// SetClock replaces the clock (tests).
func (d *Dispatcher) SetClock(now func() time.Time) { d.now = now }

// SecretAAD is the additional data of a webhook secret.
func SecretAAD(id string) string { return "webhooks|" + id + "|secret_enc" }

// Enqueue stores one delivery of body for webhook id (url is used for install
// alerts, whose webhookID is empty) and wakes the worker.
func (d *Dispatcher) Enqueue(ctx context.Context, tx *sql.Tx, webhookID, url, event string, body Body) error {
	if body.Timestamp == 0 {
		body.Timestamp = d.now().UnixMilli()
	}
	body.Type = event
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	now := d.now().UnixMilli()
	_, err = tx.ExecContext(ctx, `INSERT INTO webhook_deliveries (id, webhook_id, url, event, payload, status, attempts, next_at, created_at)
		VALUES (?, ?, ?, ?, ?, 'pending', 0, ?, ?)`, ids.New(ids.Delivery), webhookID, url, event, string(b), now, now)
	if err != nil {
		return err
	}
	d.Wake()
	return nil
}

// Wake makes the worker look for due deliveries now.
func (d *Dispatcher) Wake() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// Run sends due deliveries until ctx is done.
func (d *Dispatcher) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		if err := d.SendDue(ctx); err != nil && ctx.Err() == nil {
			d.log.Warn("webhook deliveries failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-d.wake:
		}
	}
}

type due struct {
	id, webhookID, url, event, payload string
	attempts                           int
}

// SendDue sends every delivery whose time has come, oldest first.
func (d *Dispatcher) SendDue(ctx context.Context) error {
	rows, err := store.All(ctx, d.st.Reader, func(r *sql.Rows) (due, error) {
		var x due
		return x, r.Scan(&x.id, &x.webhookID, &x.url, &x.event, &x.payload, &x.attempts)
	}, `SELECT id, webhook_id, url, event, payload, attempts FROM webhook_deliveries
		WHERE status = 'pending' AND next_at <= ? ORDER BY next_at LIMIT 50`, d.now().UnixMilli())
	if err != nil {
		return err
	}
	for _, x := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		d.deliver(ctx, x)
	}
	return nil
}

func (d *Dispatcher) deliver(ctx context.Context, x due) {
	url, secret, enabled, err := d.target(ctx, x)
	status, code, msg := "failed", 0, ""
	switch {
	case err != nil:
		msg = err.Error()
	case !enabled:
		status, msg = "failed", "The webhook is disabled."
	default:
		code, err = d.post(ctx, url, secret, x.event, []byte(x.payload))
		if err == nil && code >= 200 && code < 300 {
			status = "ok"
		} else if err != nil {
			msg = err.Error()
		} else {
			msg = fmt.Sprintf("The receiver answered HTTP %d.", code)
		}
	}
	attempts := x.attempts + 1
	next := int64(0)
	if status == "failed" && enabled && !isPermanent(err) && attempts <= Retries {
		status = "pending"
		next = d.now().Add(Backoff[attempts-1]).UnixMilli()
	}
	werr := d.st.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE webhook_deliveries SET status = ?, attempts = ?, next_at = ?, response_code = ?, error = ? WHERE id = ?`,
			status, attempts, next, code, msg, x.id)
		return err
	})
	if werr != nil {
		d.log.Warn("cannot record a webhook delivery", "id", x.id, "err", werr)
	}
}

func isPermanent(err error) bool {
	var b *ErrBlocked
	return errors.As(err, &b) || errors.Is(err, errNoWebhook)
}

var errNoWebhook error = fail.Sentence("The webhook was deleted.")

func (d *Dispatcher) target(ctx context.Context, x due) (url, secret string, enabled bool, err error) {
	if x.webhookID == "" {
		s := ""
		if d.AlertSecret != nil {
			s = d.AlertSecret()
		}
		return x.url, s, true, nil
	}
	var enc []byte
	var en int
	err = d.st.Reader.QueryRowContext(ctx, `SELECT url, secret_enc, enabled FROM webhooks WHERE id = ?`, x.webhookID).Scan(&url, &enc, &en)
	if store.NoRows(err) {
		return "", "", false, errNoWebhook
	}
	if err != nil {
		return "", "", false, err
	}
	pt, err := d.box.Open(enc, SecretAAD(x.webhookID))
	if err != nil {
		return "", "", false, fail.Sentence("The webhook secret cannot be decrypted with this install's master key.")
	}
	return url, string(pt), en == 1, nil
}

func (d *Dispatcher) post(ctx context.Context, url, secret, event string, body []byte) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fail.Sentence("The webhook URL is not valid: " + err.Error() + ".")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "5050cloud-webhook/"+version.Version)
	req.Header.Set("X-5050-Event", event)
	if secret != "" {
		req.Header.Set("X-5050-Signature", Sign(secret, body))
	}
	resp, err := d.client().Do(req)
	if err != nil {
		var b *ErrBlocked
		if errors.As(err, &b) {
			return 0, b
		}
		return 0, fail.Sentence("The request to the webhook failed: " + strings.TrimSuffix(err.Error(), ".") + ".")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// Test sends a sample event at once, without queueing or retries.
func (d *Dispatcher) Test(ctx context.Context, url, secret string, body Body) api.WebhookTestResponse {
	body.Type = "test"
	if body.Timestamp == 0 {
		body.Timestamp = d.now().UnixMilli()
	}
	if body.Details == nil {
		body.Details = map[string]any{"message": "This is a test delivery from 5050cloud."}
	}
	b, _ := json.Marshal(body)
	code, err := d.post(ctx, url, secret, "test", b)
	if err != nil {
		return api.WebhookTestResponse{Status: "failed", Error: err.Error()}
	}
	if code < 200 || code >= 300 {
		return api.WebhookTestResponse{Status: "failed", ResponseCode: code, Error: fmt.Sprintf("The receiver answered HTTP %d.", code)}
	}
	return api.WebhookTestResponse{Status: "ok", ResponseCode: code}
}

// Deliveries returns the latest n deliveries of a webhook.
func (d *Dispatcher) Deliveries(ctx context.Context, webhookID string, n int) ([]api.WebhookDelivery, error) {
	return store.All(ctx, d.st.Reader, func(r *sql.Rows) (api.WebhookDelivery, error) {
		var x api.WebhookDelivery
		return x, r.Scan(&x.ID, &x.Event, &x.Status, &x.Attempts, &x.ResponseCode, &x.CreatedAt)
	}, `SELECT id, event, status, attempts, response_code, created_at FROM webhook_deliveries WHERE webhook_id = ? ORDER BY id DESC LIMIT ?`, webhookID, n)
}

// Prune deletes finished deliveries older than 30 days.
func (d *Dispatcher) Prune(ctx context.Context) error {
	cut := d.now().Add(-30 * 24 * time.Hour).UnixMilli()
	return d.st.Exec(ctx, `DELETE FROM webhook_deliveries WHERE status != 'pending' AND created_at < ?`, cut)
}
