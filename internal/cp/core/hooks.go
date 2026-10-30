// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/core/auth"
	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/ids"
	"github.com/musakkka/5050cloud/internal/cp/core/store"
	"github.com/musakkka/5050cloud/internal/cp/core/webhooks"
	"github.com/musakkka/5050cloud/pkg/api"
)

var hookEvents = map[string]bool{
	api.HookDeploymentSuccess: true, api.HookDeploymentFailed: true, api.HookDeploymentCrashed: true, api.HookReplicaOOM: true,
	api.HookServiceUnhealthy: true, api.HookVolumeHigh: true, api.HookDiskHigh: true, api.HookBackupFailed: true,
	api.HookMonitorTriggered: true, api.HookMonitorResolved: true, api.HookHostOffline: true,
}

func (c *Core) projectWebhooks(ctx context.Context, q store.Querier, projectID string) ([]api.Webhook, error) {
	return store.All(ctx, q, func(r *sql.Rows) (api.Webhook, error) {
		var h api.Webhook
		var ev string
		var en int
		err := r.Scan(&h.ID, &h.ProjectID, &h.URL, &ev, &h.Preset, &en)
		_ = json.Unmarshal([]byte(ev), &h.Events)
		if h.Events == nil {
			h.Events = []string{}
		}
		h.Enabled = en == 1
		return h, err
	}, `SELECT id, project_id, url, events, preset, enabled FROM webhooks WHERE project_id = ? ORDER BY created_at`, projectID)
}

// Webhooks is GET /projects/{pid}/webhooks with recent deliveries.
func (c *Core) Webhooks(ctx context.Context, projectID string) ([]api.Webhook, error) {
	list, err := c.projectWebhooks(ctx, c.st.Reader, projectID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].Deliveries, _ = c.hooks.Deliveries(ctx, list[i].ID, 10)
	}
	if list == nil {
		list = []api.Webhook{}
	}
	return list, nil
}

func checkHook(h api.Webhook) error {
	u, err := url.Parse(h.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fail.Validation("url", "The webhook URL must be an http or https URL.")
	}
	if strings.Contains(u.Hostname(), "%") {
		return fail.Validation("url", "The webhook URL must not name a network interface.")
	}
	switch h.Preset {
	case "", "discord", "slack":
	default:
		return fail.Validation("preset", "The format must be empty, discord or slack.")
	}
	for _, e := range h.Events {
		if !hookEvents[e] {
			return fail.Validation("events", "%q is not a webhook event.", e)
		}
	}
	return nil
}

// CreateWebhook stores a project webhook; the secret is returned once.
func (c *Core) CreateWebhook(ctx context.Context, projectID string, h api.Webhook) (api.Webhook, error) {
	if _, err := c.Project(ctx, projectID); err != nil {
		return api.Webhook{}, err
	}
	if err := checkHook(h); err != nil {
		return api.Webhook{}, err
	}
	if h.Events == nil {
		h.Events = []string{}
	}
	h.ID, h.ProjectID, h.Enabled = ids.New(ids.Webhook), projectID, true
	secret := auth.NewToken(32)
	enc, err := c.box.Seal([]byte(secret), webhooks.SecretAAD(h.ID))
	if err != nil {
		return api.Webhook{}, err
	}
	if err := c.st.Exec(ctx, `INSERT INTO webhooks (id, project_id, url, events, preset, secret_enc, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?, 1, ?)`,
		h.ID, projectID, h.URL, jsonString(h.Events), h.Preset, enc, c.Now()); err != nil {
		return api.Webhook{}, err
	}
	h.Secret = secret
	return h, nil
}

func (c *Core) webhookByID(ctx context.Context, id string) (api.Webhook, []byte, error) {
	var h api.Webhook
	var ev string
	var en int
	var enc []byte
	err := c.st.Reader.QueryRowContext(ctx, `SELECT id, project_id, url, events, preset, enabled, secret_enc FROM webhooks WHERE id = ?`, id).
		Scan(&h.ID, &h.ProjectID, &h.URL, &ev, &h.Preset, &en, &enc)
	if store.NoRows(err) {
		return h, nil, fail.NotFound("The webhook %s does not exist.", id)
	}
	_ = json.Unmarshal([]byte(ev), &h.Events)
	h.Enabled = en == 1
	return h, enc, err
}

// WebhookProject returns the project of a webhook (access checks).
func (c *Core) WebhookProject(ctx context.Context, id string) (string, error) {
	h, _, err := c.webhookByID(ctx, id)
	return h.ProjectID, err
}

// UpdateWebhook replaces url, events, preset and enabled.
func (c *Core) UpdateWebhook(ctx context.Context, id string, in api.Webhook) (api.Webhook, error) {
	h, _, err := c.webhookByID(ctx, id)
	if err != nil {
		return h, err
	}
	if in.URL != "" {
		h.URL = in.URL
	}
	if in.Events != nil {
		h.Events = in.Events
	}
	h.Preset, h.Enabled = in.Preset, in.Enabled
	if err := checkHook(h); err != nil {
		return h, err
	}
	err = c.st.Exec(ctx, `UPDATE webhooks SET url = ?, events = ?, preset = ?, enabled = ? WHERE id = ?`, h.URL, jsonString(h.Events), h.Preset, store.Bool(h.Enabled), id)
	return h, err
}

// DeleteWebhook removes a webhook and its deliveries.
func (c *Core) DeleteWebhook(ctx context.Context, id string) error {
	if _, _, err := c.webhookByID(ctx, id); err != nil {
		return err
	}
	return c.st.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM webhook_deliveries WHERE webhook_id = ?`, id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM webhooks WHERE id = ?`, id)
		return err
	})
}

// TestWebhook sends a test delivery at once.
func (c *Core) TestWebhook(ctx context.Context, id string) (api.WebhookTestResponse, error) {
	h, enc, err := c.webhookByID(ctx, id)
	if err != nil {
		return api.WebhookTestResponse{}, err
	}
	sec, err := c.box.Open(enc, webhooks.SecretAAD(id))
	if err != nil {
		return api.WebhookTestResponse{}, fail.Internal(err)
	}
	return c.hooks.Test(ctx, h.URL, string(sec), webhooks.Body{Project: h.ProjectID}), nil
}
