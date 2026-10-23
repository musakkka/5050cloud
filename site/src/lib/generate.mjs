// SPDX-License-Identifier: MIT
// Reference pages generated from the source tree, so they cannot drift from
// the code: the REST API from pkg/api/routes.go (with the descriptions of
// docs/SPEC.md section 11 when that file is present), the service settings
// from pkg/api/types.go, and one CLI page per command from data/cli.json.

import { readFileSync, existsSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

const ROLE = { '-': 'Public', V: 'Viewer', M: 'Member', A: 'Admin', O: 'Owner' };
const ROLE_CONST = { Public: '-', Viewer: 'V', Member: 'M', Admin: 'A', Owner: 'O' };

export function parseRoutes(src) {
  const consts = {};
  for (const m of src.matchAll(/^\s*(p[A-Z]\w*)\s*=\s*"([^"]+)"/gm)) consts[m[1]] = m[2];
  const routes = [];
  let section = '';
  for (const line of src.split('\n')) {
    const sec = line.match(/^\s*\/\/\s*(11\.\d+)\s+(.*)$/);
    if (sec) { section = `${sec[1]} ${sec[2].trim()}`; continue; }
    if (!/^\s*\{Method:/.test(line)) continue;
    const method = line.match(/Method:\s*"(\w+)"/)[1];
    const pm = line.match(/Path:\s*([^,]+?),\s*(NoPrefix|Role)/);
    let path = '';
    for (const piece of pm[1].split('+').map((s) => s.trim())) {
      if (piece.startsWith('"')) path += piece.slice(1, -1);
      else if (consts[piece] !== undefined) path += consts[piece];
      else throw new Error(`unknown path constant ${piece} in: ${line.trim()}`);
    }
    const noPrefix = /NoPrefix:\s*true/.test(line);
    const roleRaw = line.match(/Role:\s*(\w+|"[^"]*")/)[1];
    const role = roleRaw.startsWith('"') ? roleRaw.slice(1, -1) : ROLE_CONST[roleRaw];
    const typ = (k) => {
      const m = line.match(new RegExp(`${k}:\\s*(Text\\("[^"]*"\\)|[\\w\\[\\]\\.]+)\\{?\\}?`));
      if (!m) return '';
      return m[1].replace(/^Text\("([^"]*)"\)$/, '$1');
    };
    const str = (k) => (line.match(new RegExp(`${k}:\\s*"([^"]*)"`)) || [])[1] || '';
    routes.push({
      section, method, path: noPrefix ? path : `/api/v1${path}`, rawPath: path, noPrefix,
      role, req: typ('Req'), resp: typ('Resp'), stream: str('Stream'), query: str('Query'),
      audit: str('Audit'),
    });
  }
  return routes;
}

// specDescriptions reads "| METHOD | `path` | description | role |" rows of
// section 11 so each generated row can carry the spec's words.
export function specDescriptions(spec) {
  const map = new Map();
  const start = spec.indexOf('## 11. REST API');
  const end = spec.indexOf('## 12. CLI');
  if (start < 0 || end < 0) return map;
  for (const line of spec.slice(start, end).split('\n')) {
    const m = line.match(/^\|\s*([A-Z]+(?: \(WebSocket\))?)\s*\|\s*`([^`]+)`[^|]*\|\s*(.*?)\s*\|\s*([^|]*)\|\s*$/);
    if (m) map.set(`${m[1].split(' ')[0]} ${m[2]}`, stripRefs(m[3]));
  }
  return map;
}

export function agentEndpoints(spec) {
  const start = spec.indexOf('### 11.6');
  const end = spec.indexOf('### 11.7');
  if (start < 0 || end < 0) return [];
  const rows = [];
  for (const line of spec.slice(start, end).split('\n')) {
    const m = line.match(/^\|\s*(GET|POST|GET \(WebSocket\))\s*\|\s*`([^`]+)`\s*\|\s*(.*?)\s*\|\s*$/);
    if (m) rows.push({ method: m[1], path: `/api/agent/v1${m[2]}`, auth: stripRefs(m[3]) });
  }
  return rows;
}

// stripRefs removes spec section numbers such as "(7.1)" or "(see 9.17)".
export function stripRefs(s) {
  return s.replace(/\s*\((?:see )?\d+\.\d+(?:\.\d+)?(?:,\s*\d+\.\d+(?:\.\d+)?)*\)/g, '');
}

function cell(s) {
  return String(s || '').replace(/\|/g, '\\|');
}

export function apiPage(root) {
  const src = readFileSync(join(root, 'pkg/api/routes.go'), 'utf8');
  const specPath = join(root, 'docs/SPEC.md');
  const spec = existsSync(specPath) ? readFileSync(specPath, 'utf8') : '';
  const routes = parseRoutes(src);
  const desc = specDescriptions(spec);
  const out = [];
  out.push('The 5050cloud REST API serves everything the console and the CLI do, with the same roles and audit log.');
  out.push('');
  out.push('## Basics');
  out.push('');
  out.push('- Base URL: `https://<your console domain>/api/v1`. Health endpoints `/healthz` and `/readyz` sit at the root.');
  out.push('- Authentication: `Authorization: Bearer 5050_...` with an API token from **Account > Tokens** or `5050 login`. Browser sessions use the `5050_session` cookie and must send the header `X-5050-CSRF: 1` on every request that changes state.');
  out.push('- Scopes: an `account` token acts as its user. An `environment` token can deploy, read logs and set variables in one environment.');
  out.push('- Roles: Public needs no login. Viewer, Member, Admin and Owner are the install roles; a project grant can replace the install role for one project.');
  out.push('- Errors are JSON `{code, message, field?}` with the codes `unauthorized`, `forbidden`, `not_found`, `conflict`, `validation`, `rate_limited`, `unavailable` (also read-only mode) and `internal`.');
  out.push('- Request bodies are capped at 1 MiB, except uploads. Unknown JSON fields are rejected.');
  out.push('- Every request that is not a GET writes an audit log entry. The **Audit** column names its action.');
  out.push('- Types named in the tables are the Go types in `pkg/api/types.go` (MIT licensed), with camelCase JSON names. TypeScript types are generated from them.');
  out.push('');
  let section = '';
  for (const r of routes) {
    if (r.section !== section) {
      section = r.section;
      out.push('');
      out.push(`## ${section.replace(/^11\.\d+\s*/, '')}`);
      out.push('');
      out.push('| Method | Path | Body and response | Role | Audit |');
      out.push('|---|---|---|---|---|');
    }
    const key = `${r.method} ${r.rawPath}`;
    let body = desc.get(key);
    if (!body) {
      const parts = [];
      if (r.req) parts.push(`\`${r.req}\``);
      parts.push(r.resp ? `\`${r.resp}\`` : '204');
      body = parts.join(' -> ');
    } else {
      body = body.replace(/^\s*-\s*$/, '');
    }
    const extra = [];
    if (r.stream === 'sse') extra.push('Server-Sent Events');
    if (r.stream === 'ws') extra.push('WebSocket');
    if (r.stream === 'redirect') extra.push('302 redirect');
    if (r.query) extra.push(`query \`${r.query}\``);
    const b = extra.length ? `${body} (${extra.join(', ')})` : body;
    out.push(`| ${r.method} | \`${cell(r.path)}\` | ${cell(b)} | ${ROLE[r.role] || r.role} | ${r.audit ? `\`${r.audit}\`` : ''} |`);
  }
  const agents = agentEndpoints(spec);
  if (agents.length) {
    out.push('');
    out.push('## Agent endpoints');
    out.push('');
    out.push('Agents call these. They are also served on `agents.<console domain>` so you can firewall them separately. See [Agent protocol](/docs/reference/agent-protocol).');
    out.push('');
    out.push('| Method | Path | Auth |');
    out.push('|---|---|---|');
    for (const a of agents) out.push(`| ${a.method} | \`${a.path}\` | ${cell(a.auth)} |`);
  }
  out.push('');
  out.push('## Next steps');
  out.push('');
  out.push('- [CLI overview](/docs/cli/overview): the CLI calls these endpoints for you.');
  out.push('- [Security model](/docs/reference/security-model): tokens, roles and the audit log.');
  return { md: out.join('\n'), routes, agents };
}

// serviceSettingsPage lists every field of api.ServiceConfig and its nested
// structs with the comment from the source.
export function serviceSettingsPage(root) {
  const src = readFileSync(join(root, 'pkg/api/types.go'), 'utf8');
  const structs = {};
  const re = /type (\w+) struct \{\n([\s\S]*?)\n\}/g;
  for (const m of src.matchAll(re)) structs[m[1]] = m[2];
  const railway = JSON.parse(readFileSync(new URL('../../data/railway-settings.json', import.meta.url), 'utf8'));
  const fields = [];
  const walk = (name, prefix) => {
    const body = structs[name];
    if (!body) return;
    let pending = [];
    for (const raw of body.split('\n')) {
      const line = raw.trim();
      if (line.startsWith('//')) { pending.push(line.replace(/^\/\/\s?/, '')); continue; }
      const f = line.match(/^(\w+)\s+([\w\*\[\]\.]+)\s+`json:"([^",]+)[^`]*`\s*(?:\/\/\s*(.*))?$/);
      if (!f) { pending = []; continue; }
      const [, , type, json, trailing] = f;
      const path = prefix ? `${prefix}.${json}` : json;
      const base = type.replace(/^[\*\[\]]+/, '');
      if (structs[base] && base !== 'Placement') {
        walk(base, path);
      } else if (base === 'Placement') {
        walk('Placement', path);
      } else {
        const note = [...pending, trailing || ''].join(' ').trim();
        fields.push({ path, type: type.replace(/^\*/, ''), note: stripRefs(note) });
      }
      pending = [];
    }
  };
  walk('ServiceConfig', '');
  const out = [];
  out.push('Every setting a service has, with its default and the Railway setting it matches.');
  out.push('');
  out.push('Settings live in the service panel under **Settings**. Edits are staged until you press **Deploy**. A field set by a config file (`railway.json`, `5050.yaml`) shows a file icon and cannot be edited in the console; see [Config as code](/docs/guides/config-as-code).');
  out.push('');
  out.push('The JSON names below are the ones the API, `5050 environment edit --service-config` and `5050.yaml` use.');
  out.push('');
  out.push('| Field | Type | Default and notes | Railway |');
  out.push('|---|---|---|---|');
  for (const f of fields) {
    out.push(`| \`${f.path}\` | ${f.type} | ${cell(f.note)} | ${cell(railway[f.path] || 'No equivalent')} |`);
  }
  out.push('');
  out.push('## Next steps');
  out.push('');
  out.push('- [Config file schema](/docs/reference/config-file): the same settings in a file.');
  out.push('- [Variables reference](/docs/reference/variables): variables that override settings.');
  return { md: out.join('\n'), fields };
}

export function cliPages() {
  const cli = JSON.parse(readFileSync(new URL('../../data/cli.json', import.meta.url), 'utf8'));
  return cli.commands.map((c) => {
    const out = [];
    out.push(c.summary);
    out.push('');
    out.push('## Usage');
    out.push('');
    out.push('```sh');
    for (const u of c.usage) out.push(u);
    out.push('```');
    if (c.flags && c.flags.length) {
      out.push('');
      out.push('## Flags');
      out.push('');
      out.push('| Flag | Meaning |');
      out.push('|---|---|');
      for (const [f, d] of c.flags) out.push(`| \`${cell(f)}\` | ${cell(d)} |`);
    }
    if (c.details && c.details.length) {
      out.push('');
      out.push('## Details');
      out.push('');
      for (const d of c.details) out.push(`- ${d}`);
    }
    if (c.examples && c.examples.length) {
      out.push('');
      out.push('## Examples');
      out.push('');
      out.push('```sh');
      for (const e of c.examples) out.push(e);
      out.push('```');
    }
    out.push('');
    out.push('## Railway equivalent');
    out.push('');
    out.push(c.railway || `\`railway ${c.name}\` works the same way through the [railway alias](/docs/cli/railway-alias).`);
    out.push('');
    out.push('Every command also accepts the [global options](/docs/cli/global-options).');
    return { slug: `docs/cli/${c.slug || c.name}`, title: `5050 ${c.name}`, description: c.summary, md: out.join('\n'), name: c.name };
  });
}

// templatePages makes one page per template in templates/*/template.yaml, at
// the docs URL the manifest names (docs/templates/<id> or docs/databases/<id>).
// It reads only the few top-level fields it shows, line by line.
export function templatePages(root) {
  const dir = join(root, 'templates');
  const pages = [];
  for (const id of readdirSync(dir).sort()) {
    const file = join(dir, id, 'template.yaml');
    if (!existsSync(file)) continue;
    const lines = readFileSync(file, 'utf8').split('\n');
    const top = {};
    const inputs = [];
    const services = [];
    let section = '';
    let folded = null;
    let svc = null;
    for (const line of lines) {
      if (/^\s*#/.test(line)) continue;
      if (folded) {
        if (/^\s{2,}\S/.test(line)) { top[folded] = `${top[folded] || ''} ${line.trim()}`.trim(); continue; }
        folded = null;
      }
      const t = line.match(/^([a-zA-Z]+):\s*(.*)$/);
      if (t) {
        section = t[1];
        if (t[2] === '>-' || t[2] === '>' || t[2] === '|') { folded = t[1]; top[t[1]] = ''; }
        else if (t[2] !== '') top[t[1]] = t[2].replace(/^["']|["']$/g, '');
        continue;
      }
      if (section === 'inputs') {
        const m = line.match(/^ {2}- name:\s*(.+)$/);
        if (m) { inputs.push({ name: m[1].trim() }); continue; }
        const f = line.match(/^ {4}(label|description|generate|required):\s*(.+)$/);
        if (f && inputs.length) inputs[inputs.length - 1][f[1]] = f[2].trim().replace(/^["']|["']$/g, '');
      }
      if (section === 'services') {
        const m = line.match(/^ {2}- name:\s*(.+)$/);
        if (m) { svc = { name: m[1].trim() }; services.push(svc); continue; }
        const img = line.match(/^ {8}image:\s*(\S+)/);
        if (img && svc && !svc.image) svc.image = img[1];
        const mp = line.match(/^ {6}mountPath:\s*(\S+)/);
        if (mp && svc) svc.volume = mp[1];
        if (/^ {4}domain:\s*true/.test(line) && svc) svc.domain = true;
      }
    }
    if (!top.id || !top.docs) continue;
    const slug = top.docs.replace(/^https:\/\/5050cloud\.com\//, '').replace(/\/$/, '');
    const out = [];
    out.push(`${top.tagline || top.name}`);
    out.push('');
    if (top.description) { out.push(top.description); out.push(''); }
    out.push('## Deploy');
    out.push('');
    if (top.category === 'database' || slug.startsWith('docs/databases/')) {
      out.push(`On the canvas, select **+ Create**, then **Database** or **Template**, then **${top.name}**. Or run \`5050 deploy --template ${top.id}\`.`);
    } else {
      out.push(`On the canvas, select **+ Create**, then **Template**, then **${top.name}**. Or run \`5050 deploy --template ${top.id}\`.`);
    }
    out.push('');
    out.push('## Services it creates');
    out.push('');
    out.push('| Service | Image | Volume | Public domain |');
    out.push('|---|---|---|---|');
    for (const s of services) out.push(`| ${cell(s.name)} | \`${cell(s.image || '')}\` | ${s.volume ? `\`${cell(s.volume)}\`` : 'none'} | ${s.domain ? 'yes' : 'no'} |`);
    out.push('');
    out.push('## Inputs');
    out.push('');
    if (inputs.length) {
      out.push('| Input | Required | Default |');
      out.push('|---|---|---|');
      for (const i of inputs) out.push(`| \`${cell(i.name)}\`${i.description ? `: ${cell(i.description)}` : ''} | ${i.required === 'true' ? 'yes' : 'no'} | ${i.generate ? `generated: \`${cell(i.generate)}\`` : ''} |`);
    } else {
      out.push('None.');
    }
    out.push('');
    out.push(`The manifest is [templates/${id}/template.yaml]({{REPO}}/blob/main/templates/${id}/template.yaml).`);
    out.push('');
    out.push('## Next steps');
    out.push('');
    out.push('- [Templates reference](/docs/reference/templates)');
    out.push('- [Backups and restore](/docs/guides/backups)');
    pages.push({ slug, id: top.id, title: top.name || top.id, description: top.tagline || `The ${top.id} template.`, md: out.join('\n') });
  }
  return pages;
}
