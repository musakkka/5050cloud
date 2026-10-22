// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"context"
	"database/sql"

	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/store"
	"github.com/musakkka/5050cloud/pkg/api"
)

// Level is an effective permission level, ordered.
type Level int

// Levels match the route table's role letters (11).
const (
	None Level = iota
	Viewer
	Member
	Admin
	Owner
)

func atLeast(l, want Level) bool { return l >= want }

// LevelOf maps a route role letter to a level.
func LevelOf(letter string) Level {
	switch letter {
	case api.Viewer:
		return Viewer
	case api.Member:
		return Member
	case api.Admin:
		return Admin
	case api.Owner:
		return Owner
	}
	return None
}

// InstallLevel is the actor's install-wide level.
func InstallLevel(a Actor) Level {
	if a.System != "" {
		return Owner
	}
	switch a.Role {
	case api.RoleOwner:
		return Owner
	case api.RoleAdmin:
		return Admin
	case api.RoleMember:
		return Member
	case api.RoleViewer:
		return Viewer
	}
	return None
}

// ProjectLevel is the actor's level on one project: Owner and Admin keep
// full access; a project grant replaces the install role of Members and
// Viewers; Members default to editor and Viewers to viewer (9.15).
func (c *Core) ProjectLevel(ctx context.Context, a Actor, projectID string) (Level, error) {
	base := InstallLevel(a)
	if base >= Admin || a.UserID == "" {
		return base, nil
	}
	var role string
	err := c.st.Reader.QueryRowContext(ctx, `SELECT role FROM project_grants WHERE project_id = ? AND user_id = ?`, projectID, a.UserID).Scan(&role)
	if store.NoRows(err) {
		return base, nil
	}
	if err != nil {
		return None, err
	}
	switch api.ProjectRole(role) {
	case api.ProjectEditor:
		return Member, nil
	case api.ProjectViewer:
		return Viewer, nil
	}
	return None, nil
}

// VisibleProjects returns the ids of projects the actor may see, or nil for all.
func (c *Core) hiddenProjects(ctx context.Context, a Actor) (map[string]bool, error) {
	if InstallLevel(a) >= Admin {
		return nil, nil
	}
	ids, err := store.Strings(ctx, c.st.Reader, `SELECT project_id FROM project_grants WHERE user_id = ? AND role = 'none'`, a.UserID)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// ProjectGrants is GET /projects/{pid}/members: every user with their
// effective role on the project.
func (c *Core) ProjectGrants(ctx context.Context, projectID string) ([]api.ProjectGrant, error) {
	if _, err := c.Project(ctx, projectID); err != nil {
		return nil, err
	}
	grants := map[string]string{}
	rows, err := store.All(ctx, c.st.Reader, func(r *sql.Rows) ([2]string, error) {
		var x [2]string
		return x, r.Scan(&x[0], &x[1])
	}, `SELECT user_id, role FROM project_grants WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		grants[r[0]] = r[1]
	}
	users, err := c.Members(ctx)
	if err != nil {
		return nil, err
	}
	out := []api.ProjectGrant{}
	for _, u := range users {
		if u.Role == api.RoleOwner || u.Role == api.RoleAdmin {
			continue
		}
		role := api.ProjectEditor
		if u.Role == api.RoleViewer {
			role = api.ProjectViewer
		}
		if g, ok := grants[u.ID]; ok {
			role = api.ProjectRole(g)
		}
		out = append(out, api.ProjectGrant{UserID: u.ID, Email: u.Email, Name: u.Name, Role: role})
	}
	return out, nil
}

// SetProjectGrants replaces the grants of a project.
func (c *Core) SetProjectGrants(ctx context.Context, projectID string, in []api.ProjectGrant) ([]api.ProjectGrant, error) {
	if _, err := c.Project(ctx, projectID); err != nil {
		return nil, err
	}
	err := c.st.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM project_grants WHERE project_id = ?`, projectID); err != nil {
			return err
		}
		for i, g := range in {
			switch g.Role {
			case api.ProjectEditor, api.ProjectViewer, api.ProjectNone:
			default:
				return fail.Validation("role", "Grant %d has the role %q; use editor, viewer or none.", i+1, g.Role)
			}
			u, err := c.userByID(ctx, tx, g.UserID)
			if err != nil {
				return fail.Validation("userId", "The user %s does not exist.", g.UserID)
			}
			if u.Role == api.RoleOwner || u.Role == api.RoleAdmin {
				continue // owners and admins always keep full access
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO project_grants (project_id, user_id, role) VALUES (?, ?, ?)`, projectID, g.UserID, string(g.Role)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return c.ProjectGrants(ctx, projectID)
}
