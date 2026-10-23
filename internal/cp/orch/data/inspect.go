// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
)

// The Data tab reads databases through commands run inside the running
// replica (docs/SPEC.md 9.13). Identifiers (user, database) come from the
// service's stored variables, never from the request; values from the request
// (a key pattern, a key, an extension name) travel as separate argv elements
// or script arguments, never through a shell.

const (
	inspectTimeoutSec = 30
	revealLimit       = 1024
	maxTables         = 500
)

// inspectTarget loads a running data service of one of the given kinds.
func (d *Data) inspectTarget(ctx context.Context, envID, serviceID string, kinds ...string) (cpapi.ServiceSnapshot, recipeInfo, map[string]string, error) {
	snap, err := d.instance(ctx, envID, serviceID)
	if err != nil {
		return snap, recipeInfo{}, nil, err
	}
	info := d.recipe(snap)
	if info.data == "" {
		return snap, info, nil, errors.New("This service is not a database, so it has no Data tab.")
	}
	if len(kinds) > 0 && !contains(kinds, info.data) {
		return snap, info, nil, fmt.Errorf("This is only available for %s.", strings.Join(kinds, " and "))
	}
	return snap, info, d.serviceEnv(ctx, snap), nil
}

// runInspect execs one inspection command and decodes its JSON output.
func (d *Data) runInspect(ctx context.Context, snap cpapi.ServiceSnapshot, what string, cmd []string, env map[string]string, stdin []byte, v any) error {
	out := d.execInService(ctx, snap, "inspect", cmd, env, "", stdin, inspectTimeoutSec)
	if !out.ok() {
		return errors.New(jobError(what, out))
	}
	if err := decodeOutput(out.Result.Output, v); err != nil {
		return fmt.Errorf("%s returned output that could not be read: %w.", what, err)
	}
	return nil
}

// decodeOutput finds the JSON value in a command's combined output: the last
// line that parses, or the whole output from its first brace.
func decodeOutput(output string, v any) error {
	lines := strings.Split(strings.TrimRight(output, "\r\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if (strings.HasPrefix(l, "{") || strings.HasPrefix(l, "[")) && json.Unmarshal([]byte(l), v) == nil {
			return nil
		}
	}
	if i := strings.IndexAny(output, "{["); i >= 0 {
		if err := json.Unmarshal([]byte(strings.TrimSpace(output[i:])), v); err == nil {
			return nil
		}
	}
	return errors.New("no JSON value in the output")
}

// ---------------------------------------------------------------- per engine commands

func psqlCmd(env map[string]string) ([]string, map[string]string) {
	user := pick(env, "POSTGRES_USER", "PGUSER")
	if user == "" {
		user = "postgres"
	}
	db := pick(env, "POSTGRES_DB", "PGDATABASE")
	if db == "" {
		db = user
	}
	return []string{"psql", "-X", "-q", "-At", "-v", "ON_ERROR_STOP=1", "-h", "/var/run/postgresql", "-U", user, "-d", db},
		map[string]string{"PGPASSWORD": pick(env, "POSTGRES_PASSWORD", "PGPASSWORD"), "PGCONNECT_TIMEOUT": "10"}
}

func mysqlCmd(env map[string]string) ([]string, map[string]string) {
	user := pick(env, "MYSQLUSER")
	if user == "" {
		user = "root"
	}
	pass := pick(env, "MYSQLPASSWORD", "MYSQL_ROOT_PASSWORD")
	return []string{"mysql", "-h", "127.0.0.1", "-u", user, "-N", "-B", "--raw"}, map[string]string{"MYSQL_PWD": pass}
}

// mongoCmd runs a script that authenticates from the environment, so no
// password is on argv.
func mongoCmd(script string) ([]string, map[string]string) {
	auth := `const __a = db.getSiblingDB("admin"); if (process.env.MONGO_INITDB_ROOT_USERNAME) { __a.auth(process.env.MONGO_INITDB_ROOT_USERNAME, process.env.MONGO_INITDB_ROOT_PASSWORD); }`
	return []string{"mongosh", "--quiet", "--norc", "mongodb://127.0.0.1:27017/admin", "--eval", auth + "\n" + script}, nil
}

func redisCmd(env map[string]string, args ...string) ([]string, map[string]string) {
	argv := append([]string{"redis-cli", "-h", "127.0.0.1", "-p", "6379", "--no-auth-warning", "--raw"}, args...)
	return argv, map[string]string{"REDISCLI_AUTH": pick(env, "REDIS_PASSWORD", "REDISPASSWORD")}
}

const pgSummarySQL = `SELECT json_build_object(
 'version', split_part(current_setting('server_version'), ' ', 1),
 'size', pg_database_size(current_database()),
 'connections', (SELECT count(*) FROM pg_stat_activity WHERE backend_type = 'client backend'),
 'tables', COALESCE((SELECT json_agg(t) FROM (
   SELECT n.nspname AS schema, c.relname AS name, GREATEST(c.reltuples, 0)::bigint AS rows,
          pg_total_relation_size(c.oid) AS bytes
   FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
   WHERE c.relkind IN ('r', 'p', 'm') AND n.nspname NOT IN ('pg_catalog', 'information_schema')
     AND n.nspname NOT LIKE 'pg_toast%' AND n.nspname NOT LIKE 'pg_temp%'
   ORDER BY pg_total_relation_size(c.oid) DESC LIMIT 500) t), '[]'::json)
)::jsonb;
`

const mysqlSummarySQL = `SELECT JSON_OBJECT(
 'version', SUBSTRING_INDEX(VERSION(), '-', 1),
 'size', (SELECT COALESCE(SUM(DATA_LENGTH + INDEX_LENGTH), 0) FROM information_schema.TABLES WHERE TABLE_SCHEMA NOT IN ('mysql', 'sys', 'performance_schema', 'information_schema')),
 'connections', (SELECT VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME = 'Threads_connected'),
 'tables', COALESCE((SELECT JSON_ARRAYAGG(JSON_OBJECT('schema', s, 'name', n, 'rows', r, 'bytes', b)) FROM (
   SELECT TABLE_SCHEMA AS s, TABLE_NAME AS n, COALESCE(TABLE_ROWS, 0) AS r, COALESCE(DATA_LENGTH + INDEX_LENGTH, 0) AS b
   FROM information_schema.TABLES
   WHERE TABLE_TYPE = 'BASE TABLE' AND TABLE_SCHEMA NOT IN ('mysql', 'sys', 'performance_schema', 'information_schema')
   ORDER BY b DESC LIMIT 500) t), JSON_ARRAY())
);
`

const mongoSummaryJS = `const out = { version: __a.version(), size: 0, connections: 0, tables: [] };
try { out.connections = __a.serverStatus().connections.current; } catch (e) {}
for (const d of __a.adminCommand({ listDatabases: 1 }).databases) {
  if (["admin", "local", "config"].includes(d.name)) continue;
  out.size += d.sizeOnDisk || 0;
  const sdb = __a.getSiblingDB(d.name);
  for (const c of sdb.getCollectionInfos({ type: "collection" })) {
    if (c.name.startsWith("system.") || out.tables.length >= 500) continue;
    let s = {};
    try { s = sdb.runCommand({ collStats: c.name }); } catch (e) {}
    out.tables.push({ schema: d.name, name: c.name, rows: s.count || 0, bytes: (s.storageSize || 0) + (s.totalIndexSize || 0) });
  }
}
print(EJSON.stringify(out, { relaxed: true }));`

type summaryJSON struct {
	Version     string      `json:"version"`
	Size        json.Number `json:"size"`
	Connections json.Number `json:"connections"`
	Tables      []struct {
		Schema string      `json:"schema"`
		Name   string      `json:"name"`
		Rows   json.Number `json:"rows"`
		Bytes  json.Number `json:"bytes"`
	} `json:"tables"`
}

func num(n json.Number) int64 {
	if n == "" {
		return 0
	}
	if v, err := n.Int64(); err == nil {
		return v
	}
	f, _ := n.Float64()
	return int64(f)
}

// DataSummary is the Data tab: tables or collections with sizes, the
// database size, the connection count and the server version. Redis has its
// own key browser (RedisKeys).
func (d *Data) DataSummary(ctx context.Context, envID, serviceID string) (api.DataResponse, error) {
	snap, info, env, err := d.inspectTarget(ctx, envID, serviceID)
	if err != nil {
		return api.DataResponse{}, err
	}
	var s summaryJSON
	kind := "table"
	switch info.data {
	case "postgres":
		cmd, cenv := psqlCmd(env)
		err = d.runInspect(ctx, snap, "Reading the tables", cmd, cenv, []byte(pgSummarySQL), &s)
	case "mysql":
		cmd, cenv := mysqlCmd(env)
		err = d.runInspect(ctx, snap, "Reading the tables", cmd, cenv, []byte(mysqlSummarySQL), &s)
	case "mongodb":
		kind = "collection"
		cmd, cenv := mongoCmd(mongoSummaryJS)
		err = d.runInspect(ctx, snap, "Reading the collections", cmd, cenv, nil, &s)
	case "redis":
		return d.redisSummary(ctx, snap, env)
	default:
		return api.DataResponse{}, fmt.Errorf("The Data tab does not support %s yet.", info.data)
	}
	if err != nil {
		return api.DataResponse{}, err
	}
	resp := api.DataResponse{Tables: []api.DataTable{}, Version: s.Version, SizeBytes: num(s.Size), Connections: int(num(s.Connections))}
	for i, t := range s.Tables {
		if i >= maxTables {
			break
		}
		resp.Tables = append(resp.Tables, api.DataTable{Name: t.Name, Schema: t.Schema, Kind: kind, Rows: num(t.Rows), Bytes: num(t.Bytes)})
	}
	return resp, nil
}

const redisInfoLua = `local i = redis.call('INFO', 'server') .. redis.call('INFO', 'clients') .. redis.call('INFO', 'memory')
return cjson.encode({ info = i, keys = redis.call('DBSIZE') })`

func (d *Data) redisSummary(ctx context.Context, snap cpapi.ServiceSnapshot, env map[string]string) (api.DataResponse, error) {
	cmd, cenv := redisCmd(env, "EVAL", redisInfoLua, "0")
	var s struct {
		Info string `json:"info"`
		Keys int64  `json:"keys"`
	}
	if err := d.runInspect(ctx, snap, "Reading Redis", cmd, cenv, nil, &s); err != nil {
		return api.DataResponse{}, err
	}
	fields := map[string]string{}
	for _, l := range strings.Split(s.Info, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), ":"); ok {
			fields[k] = v
		}
	}
	conns, _ := strconv.Atoi(fields["connected_clients"])
	size, _ := strconv.ParseInt(fields["used_memory"], 10, 64)
	return api.DataResponse{
		Tables:      []api.DataTable{{Name: "db0", Kind: "keyspace", Rows: s.Keys}},
		SizeBytes:   size,
		Connections: conns,
		Version:     fields["redis_version"],
	}, nil
}

// ---------------------------------------------------------------- Postgres extensions

const pgExtensionsSQL = `SELECT COALESCE(json_agg(json_build_object('name', name, 'version', COALESCE(default_version, ''),
 'installed', installed_version IS NOT NULL, 'comment', COALESCE(comment, '')) ORDER BY name), '[]'::json)::jsonb
FROM pg_available_extensions;
`

// Extensions lists the extensions the database's image offers and whether
// each is installed in the service's database.
func (d *Data) Extensions(ctx context.Context, envID, serviceID string) ([]api.Extension, error) {
	snap, _, env, err := d.inspectTarget(ctx, envID, serviceID, "postgres")
	if err != nil {
		return nil, err
	}
	cmd, cenv := psqlCmd(env)
	var list []api.Extension
	if err := d.runInspect(ctx, snap, "Listing the extensions", cmd, cenv, []byte(pgExtensionsSQL), &list); err != nil {
		return nil, err
	}
	if list == nil {
		list = []api.Extension{}
	}
	return list, nil
}

// pgIdent quotes an identifier for SQL.
func pgIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// EnableExtension runs CREATE EXTENSION for one of the available extensions.
// The name must match an available extension exactly; it is then quoted.
func (d *Data) EnableExtension(ctx context.Context, envID, serviceID, name, actor string) (api.Extension, error) {
	list, err := d.Extensions(ctx, envID, serviceID)
	if err != nil {
		return api.Extension{}, err
	}
	var ext *api.Extension
	for i := range list {
		if list[i].Name == name {
			ext = &list[i]
		}
	}
	if ext == nil {
		return api.Extension{}, fmt.Errorf("The extension %q is not available in this database's image.", name)
	}
	if ext.Installed {
		return *ext, nil
	}
	snap, _, env, err := d.inspectTarget(ctx, envID, serviceID, "postgres")
	if err != nil {
		return api.Extension{}, err
	}
	cmd, cenv := psqlCmd(env)
	sqlText := "CREATE EXTENSION IF NOT EXISTS " + pgIdent(ext.Name) + " CASCADE;\nSELECT '{\"ok\":true}';\n"
	var ok struct {
		OK bool `json:"ok"`
	}
	if err := d.runInspect(ctx, snap, "Enabling "+ext.Name, cmd, cenv, []byte(sqlText), &ok); err != nil {
		return api.Extension{}, err
	}
	ext.Installed = true
	d.activity(ctx, envID, serviceID, fmt.Sprintf("Enabled the %s extension in %s.", ext.Name, snap.Config.Name), actor, "")
	return *ext, nil
}

// ---------------------------------------------------------------- Redis key browser

const redisScanLua = `local r = redis.call('SCAN', ARGV[1], 'MATCH', ARGV[2], 'COUNT', 200)
local keys = {}
for _, k in ipairs(r[2]) do
  local t = redis.call('TYPE', k)['ok']
  local ttl = redis.call('PTTL', k)
  local ok, size = pcall(function() return redis.call('MEMORY', 'USAGE', k) end)
  if not ok or not size then size = 0 end
  table.insert(keys, { key = k, type = t, ttlMs = ttl, sizeBytes = size })
end
return cjson.encode({ cursor = r[1], keys = keys })`

// RedisKeys returns one SCAN page with each key's type, TTL and size.
func (d *Data) RedisKeys(ctx context.Context, envID, serviceID, match, cursor string) (api.RedisKeysPage, error) {
	snap, _, env, err := d.inspectTarget(ctx, envID, serviceID, "redis")
	if err != nil {
		return api.RedisKeysPage{}, err
	}
	if match == "" {
		match = "*"
	}
	if len(match) > 512 {
		return api.RedisKeysPage{}, errors.New("The key pattern is longer than 512 characters.")
	}
	if cursor == "" {
		cursor = "0"
	}
	if _, err := strconv.ParseUint(cursor, 10, 64); err != nil {
		return api.RedisKeysPage{}, errors.New("The cursor must be the number the previous page returned.")
	}
	cmd, cenv := redisCmd(env, "EVAL", redisScanLua, "0", cursor, match)
	var page struct {
		Cursor string          `json:"cursor"`
		Keys   json.RawMessage `json:"keys"`
	}
	if err := d.runInspect(ctx, snap, "Listing the keys", cmd, cenv, nil, &page); err != nil {
		return api.RedisKeysPage{}, err
	}
	out := api.RedisKeysPage{Keys: []api.RedisKey{}}
	// cjson encodes an empty table as {} rather than [].
	if t := strings.TrimSpace(string(page.Keys)); t != "" && t != "{}" {
		if err := json.Unmarshal(page.Keys, &out.Keys); err != nil {
			return api.RedisKeysPage{}, fmt.Errorf("Listing the keys returned output that could not be read: %w.", err)
		}
	}
	for i := range out.Keys {
		if out.Keys[i].TTLMs < 0 {
			out.Keys[i].TTLMs = -1 // no expiry
		}
	}
	if page.Cursor != "0" {
		out.Cursor = page.Cursor
	}
	return out, nil
}

const redisRevealLua = `local k = KEYS[1]
local t = redis.call('TYPE', k)['ok']
local v
if t == 'string' then
  return cjson.encode({ type = t, value = redis.call('GETRANGE', k, 0, ARGV[1] - 1), total = redis.call('STRLEN', k) })
elseif t == 'hash' then v = redis.call('HSCAN', k, 0, 'COUNT', 50)[2]
elseif t == 'list' then v = redis.call('LRANGE', k, 0, 49)
elseif t == 'set' then v = redis.call('SSCAN', k, 0, 'COUNT', 50)[2]
elseif t == 'zset' then v = redis.call('ZRANGE', k, 0, 49, 'WITHSCORES')
elseif t == 'stream' then
  v = {}
  for _, e in ipairs(redis.call('XRANGE', k, '-', '+', 'COUNT', 10)) do table.insert(v, { id = e[1], fields = e[2] }) end
elseif t == 'none' then return cjson.encode({ type = t })
else v = {} end
return cjson.encode({ type = t, value = cjson.encode(v), total = -1 })`

// RevealRedisKey returns the first 1 KB of one key's value. Strings are
// returned as stored; other types as JSON of their first elements.
func (d *Data) RevealRedisKey(ctx context.Context, envID, serviceID, key string) (api.RedisRevealResponse, error) {
	snap, _, env, err := d.inspectTarget(ctx, envID, serviceID, "redis")
	if err != nil {
		return api.RedisRevealResponse{}, err
	}
	if key == "" {
		return api.RedisRevealResponse{}, errors.New("Pick a key to reveal.")
	}
	cmd, cenv := redisCmd(env, "EVAL", redisRevealLua, "1", key, strconv.Itoa(revealLimit))
	var r struct {
		Type  string `json:"type"`
		Value string `json:"value"`
		Total int64  `json:"total"`
	}
	if err := d.runInspect(ctx, snap, "Reading the key", cmd, cenv, nil, &r); err != nil {
		return api.RedisRevealResponse{}, err
	}
	if r.Type == "none" {
		return api.RedisRevealResponse{}, errors.New("That key does not exist any more.")
	}
	out := api.RedisRevealResponse{Value: r.Value}
	if len(out.Value) > revealLimit {
		out.Value = truncateUTF8(out.Value, revealLimit)
		out.Truncated = true
	}
	if r.Total > int64(len(r.Value)) {
		out.Truncated = true
	}
	return out, nil
}

// truncateUTF8 cuts s to at most n bytes without splitting a character.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}
