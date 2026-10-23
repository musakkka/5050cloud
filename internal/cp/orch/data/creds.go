// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
)

const secretAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// newSecret returns n characters from A-Za-z0-9 (the ${{secret(n)}} alphabet).
func newSecret(n int) string {
	var b strings.Builder
	max := big.NewInt(int64(len(secretAlphabet)))
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic("data: the system random source failed: " + err.Error())
		}
		b.WriteByte(secretAlphabet[v.Int64()])
	}
	return b.String()
}

// credentialPlan says how one engine changes its password.
type credentialPlan struct {
	variable string // the variable that holds the password
	apply    func(newPass string) (cmd []string, env map[string]string, stdin []byte)
}

func (d *Data) credentialPlan(info recipeInfo, env map[string]string) (credentialPlan, error) {
	switch info.data {
	case "postgres":
		user := pick(env, "POSTGRES_USER", "PGUSER")
		if user == "" {
			user = "postgres"
		}
		return credentialPlan{variable: "POSTGRES_PASSWORD", apply: func(p string) ([]string, map[string]string, []byte) {
			cmd, cenv := psqlCmd(env)
			return cmd, cenv, []byte("ALTER ROLE " + pgIdent(user) + " WITH PASSWORD '" + p + "';\nSELECT '{\"ok\":true}';\n")
		}}, nil
	case "mysql":
		return credentialPlan{variable: "MYSQL_ROOT_PASSWORD", apply: func(p string) ([]string, map[string]string, []byte) {
			cmd, cenv := mysqlCmd(env)
			sqlText := "ALTER USER IF EXISTS 'root'@'%' IDENTIFIED BY '" + p + "';\n" +
				"ALTER USER IF EXISTS 'root'@'localhost' IDENTIFIED BY '" + p + "';\nSELECT '{\"ok\":true}';\n"
			return cmd, cenv, []byte(sqlText)
		}}, nil
	case "redis":
		return credentialPlan{variable: "REDIS_PASSWORD", apply: func(p string) ([]string, map[string]string, []byte) {
			cmd, cenv := redisCmd(env)
			// redis-cli reads commands from stdin, so the password is never on argv.
			return cmd, cenv, []byte("CONFIG SET requirepass " + p + "\nACL SETUSER default on resetpass >" + p + "\nECHO '{\"ok\":true}'\n")
		}}, nil
	case "mongodb":
		user := pick(env, "MONGO_INITDB_ROOT_USERNAME", "MONGOUSER")
		return credentialPlan{variable: "MONGO_INITDB_ROOT_PASSWORD", apply: func(p string) ([]string, map[string]string, []byte) {
			cmd, _ := mongoCmd(`__a.changeUserPassword(process.env.FIFTY_USER, process.env.FIFTY_NEW_PASSWORD); print('{"ok":true}');`)
			return cmd, map[string]string{"FIFTY_USER": user, "FIFTY_NEW_PASSWORD": p}, nil
		}}, nil
	}
	return credentialPlan{}, errors.New("Only Postgres, MySQL, Redis and MongoDB credentials can be regenerated.")
}

// RegenerateCredentials sets a new password in the database, then commits it
// to the service's variables, which redeploys the service and every service
// that references them in one commit (docs/SPEC.md 9.13).
func (d *Data) RegenerateCredentials(ctx context.Context, envID, serviceID, actor string) (api.CommitResponse, error) {
	snap, info, env, err := d.inspectTarget(ctx, envID, serviceID)
	if err != nil {
		return api.CommitResponse{}, err
	}
	plan, err := d.credentialPlan(info, env)
	if err != nil {
		return api.CommitResponse{}, err
	}
	old := env[plan.variable]
	if err := d.claim(envID, serviceID, "regenerating credentials"); err != nil {
		return api.CommitResponse{}, err
	}
	defer d.release(envID, serviceID)
	newPass := newSecret(32)
	if err := d.applyPassword(ctx, snap, plan, newPass); err != nil {
		return api.CommitResponse{}, err
	}
	resp, err := d.cfg.CommitVariables(ctx, envID, serviceID, map[string]string{plan.variable: newPass}, actor,
		"Regenerate the credentials of "+snap.Config.Name)
	if err != nil {
		// Put the old password back so the stored variables stay right.
		if old != "" {
			if rerr := d.applyPassword(ctx, snap, plan, old); rerr != nil {
				return api.CommitResponse{}, fmt.Errorf("The new password was set in the database but could not be saved, and the old one could not be restored (%w). Set %s to the value in the database by hand: %w", rerr, plan.variable, err)
			}
		}
		return api.CommitResponse{}, fmt.Errorf("The new password could not be saved, so the old one was put back: %w", err)
	}
	d.activity(ctx, envID, serviceID, fmt.Sprintf("Regenerated the credentials of %s; services that reference them redeploy.", snap.Config.Name), actor, resp.CommitID)
	return resp, nil
}

func (d *Data) applyPassword(ctx context.Context, snap cpapi.ServiceSnapshot, plan credentialPlan, pass string) error {
	cmd, env, stdin := plan.apply(pass)
	var ok struct {
		OK bool `json:"ok"`
	}
	return d.runInspect(ctx, snap, "Changing the password", cmd, env, stdin, &ok)
}
