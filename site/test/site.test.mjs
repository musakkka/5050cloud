// SPDX-License-Identifier: MIT
// Builds the site into a temp dir and checks the acceptance rules of
// docs/SPEC.md section 14 and the WP9 entry: every page exists with content,
// every CLI command and API endpoint is documented, claims are mapped, links
// resolve, and the public-text rules hold.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, readdirSync, statSync, existsSync } from 'node:fs';
import { join, dirname, resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';
import { build, rel } from '../build.mjs';
import { parseRoutes } from '../src/lib/generate.mjs';
import { checkShape, checkClaims } from '../src/lib/claims.mjs';
import { render, inline } from '../src/lib/markdown.mjs';

const SITE = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const ROOT = resolve(SITE, '..');
const OUT = mkdtempSync(join(tmpdir(), '5050site-'));
const { pages } = build(OUT);
const SPEC = existsSync(join(ROOT, 'docs/SPEC.md')) ? readFileSync(join(ROOT, 'docs/SPEC.md'), 'utf8') : null;

function walk(dir) {
  return readdirSync(dir).flatMap((n) => {
    const p = join(dir, n);
    return statSync(p).isDirectory() ? walk(p) : [p];
  });
}
const files = walk(OUT);
const htmlFiles = files.filter((f) => f.endsWith('.html'));
const read = (rel) => readFileSync(join(OUT, rel), 'utf8');
const textOf = (html) => html.replace(/<script[\s\S]*?<\/script>/g, '').replace(/<[^>]+>/g, ' ').replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/\s+/g, ' ');

// The pages section 14.2 requires, as slugs.
const REQUIRED = [
  'quick-start', 'basics', 'coming-from-railway', 'production-checklist',
  ...['deploy-from-github', 'deploy-docker-image', 'deploy-with-cli', 'config-as-code', 'import-from-railway', 'move-database-from-railway', 'import-docker-compose', 'templates', 'databases', 'variables', 'staged-changes', 'custom-domains', 'use-all-cores', 'add-a-host', 'replicas-zero-downtime', 'health-checks', 'rollbacks', 'cron-jobs', 'volumes', 'backups', 'database-major-upgrades', 'monorepos', 'pre-deploy', 'private-networking', 'quarantined-environments', 'monitors', 'email-notifications', 'members-roles-2fa', 'upgrading', 'control-plane-backup', 'rebuild-control-host', 'rotate-master-key', 'behind-cloudflare', 'troubleshooting'].map((s) => `guides/${s}`),
  ...['install-options', 'service-settings', 'config-file', 'variables', 'deployment-statuses', 'log-filters', 'templates', 'api', 'agent-protocol', 'architecture', 'security-model', 'ports-firewall', 'limits', 'honest-limits'].map((s) => `reference/${s}`),
  'cli/overview', 'cli/global-options', 'cli/railway-alias',
  ...['railway', 'coolify', 'dokploy', 'kamal', 'dokku', 'vps'].map((s) => `compare/${s}`),
  ...['build-from-source', 'makefile', 'writing-a-template'].map((s) => `contribute/${s}`),
].map((s) => `docs/${s}`);

test('every page in section 14.2 exists as HTML and Markdown with real content', () => {
  for (const slug of REQUIRED) {
    assert.ok(existsSync(join(OUT, slug, 'index.html')), `${slug}/index.html`);
    const md = read(`${slug}.md`);
    const words = md.split(/\s+/).length;
    assert.ok(words >= 60, `${slug} has only ${words} words`);
    assert.doesNotMatch(md, /lorem ipsum|coming soon|\bTODO\b|\bTBD\b/i, slug);
  }
});

test('the landing page, /deploy, /install.sh, llms.txt and llms-full.txt are built', () => {
  for (const f of ['index.html', 'deploy/index.html', 'install.sh', 'llms.txt', 'llms-full.txt', 'sitemap.xml', '404.html', 'button.svg']) {
    assert.ok(existsSync(join(OUT, f)), f);
  }
  assert.equal(read('install.sh'), readFileSync(join(ROOT, 'install/install.sh'), 'utf8'));
  const llms = read('llms.txt');
  for (const slug of REQUIRED) assert.ok(llms.includes(`https://5050cloud.com/${slug}.md`), `llms.txt lists ${slug}`);
  assert.ok(read('llms-full.txt').length > 50000);
});

test('landing sections follow section 14.1', () => {
  const html = read('index.html');
  const text = textOf(html);
  assert.ok(text.includes("Railway's developer experience on servers you own."));
  assert.ok(text.includes('curl -fsSL https://5050cloud.com/install.sh | sudo sh'));
  for (const h of ['Why it costs less', 'Use every core you pay for', 'Add servers with one command', 'Reliable by default', 'Move from Railway in minutes', 'No lock-in', 'Compared with', 'Honest limits', 'FAQ']) {
    assert.ok(html.includes(`>${h}</h2>`), h);
  }
  assert.ok(html.indexOf('Why it costs less') < html.indexOf('Honest limits'));
  assert.ok(/check current prices/i.test(text));
  assert.doesNotMatch(text, /cheaper for everyone|github stars|\bstars\b/i);
});

test('every landing claim is listed in claims.json with evidence and a condition', () => {
  const claims = JSON.parse(readFileSync(join(SITE, 'claims.json'), 'utf8'));
  let known = null;
  if (SPEC) known = new Set([...SPEC.matchAll(/^\| (E\d+[a-z]?) \|/gm)].map((m) => m[1]));
  assert.deepEqual(checkShape(claims, known), []);
  const text = textOf(read('index.html'));
  for (const c of claims.claims) assert.ok(text.includes(c.claim), `claim ${c.id} is on the page`);
});

test('verify-claims flags missing and failing tests', () => {
  const claims = { claims: [{ id: 'a', claim: 'x', evidence: ['E1', 'E2', 'source:https://example.com'], condition: 'c' }] };
  assert.deepEqual(checkClaims(claims, { tests: [{ id: 'E1', pass: true }, { id: 'E2', pass: true }] }), []);
  assert.equal(checkClaims(claims, { tests: [{ id: 'E1', pass: true }, { id: 'E2', pass: false }] }).length, 1);
  assert.equal(checkClaims(claims, [{ id: 'E1', status: 'pass' }]).length, 1);
});

test('every row of section 14.3 is on the Coming from Railway page', () => {
  const md = read('docs/coming-from-railway.md');
  const rows = ['PR environments', 'Wait for CI', 'Deployment approvals', 'Image Auto Updates', 'Serverless (app sleeping)', 'Buckets', 'Functions', 'Observability widget board', 'AI Diagnose and fix PR', 'Data tab SQL editor', 'Metrics for 30 days', 'Wildcard custom domains', 'Static egress IPs', 'Volume Live Resize', 'Regions', '`railway` CLI', 'Login with GitHub', 'Slack/Discord formatted webhooks', '`config plan/apply`'];
  for (const r of rows) assert.ok(md.includes(`| ${r}`), r);
  if (SPEC) {
    const start = SPEC.indexOf('### 14.3');
    const section = SPEC.slice(start, SPEC.indexOf('\n---', start));
    const specRows = section.split('\n').filter((l) => l.startsWith('| ') && !l.startsWith('| Railway') && !l.startsWith('|---'));
    assert.equal(specRows.length, rows.length);
    const plain = md.replace(/\[([^\]]+)\]\([^)]+\)/g, '$1');
    for (const l of specRows) assert.ok(plain.includes(l.trim()), `row from the spec: ${l}`);
  }
});

test('every endpoint in pkg/api/routes.go and the agent endpoints are documented', () => {
  const api = read('docs/reference/api.md');
  const routes = parseRoutes(readFileSync(join(ROOT, 'pkg/api/routes.go'), 'utf8'));
  assert.ok(routes.length > 100);
  for (const r of routes) assert.ok(api.includes(`| ${r.method} | \`${r.path.replace(/\|/g, '\\|')}\``), `${r.method} ${r.path}`);
  for (const p of ['/api/agent/v1/join', '/api/agent/v1/connect', '/api/agent/v1/uploads/{id}']) assert.ok(api.includes(p), p);
});

test('every CLI command of section 12 has its own page', () => {
  const cli = JSON.parse(readFileSync(join(SITE, 'data/cli.json'), 'utf8')).commands.map((c) => c.name);
  for (const name of cli) assert.ok(existsSync(join(OUT, `docs/cli/${name}/index.html`)), name);
  if (SPEC) {
    const start = SPEC.indexOf('## 12. CLI');
    const section = SPEC.slice(start, SPEC.indexOf('## 13.', start));
    const names = new Set();
    for (const m of section.matchAll(/^\| `([^`]+)`(?: \/ `([^`]+)`)?(?:, `([^`]+)`)*([^|]*)\|/gm)) {
      const cell = m[0].split('|')[1].split('(')[0];
      for (const c of cell.matchAll(/`([a-z-]+)[^`]*`/g)) names.add(c[1]);
    }
    for (const n of names) assert.ok(cli.includes(n), `spec command ${n} is documented`);
  }
});

test('every service setting is in the Service settings reference', () => {
  const md = read('docs/reference/service-settings.md');
  for (const f of ['source.repo', 'build.builder', 'deploy.processes', 'deploy.placement.roles', 'networking.port', 'deploy.memoryReservationMb', 'build.timeoutMin']) {
    assert.ok(md.includes(`\`${f}\``), f);
  }
});

test('internal links and assets resolve', () => {
  const missing = [];
  for (const f of htmlFiles) {
    const html = readFileSync(f, 'utf8');
    for (const m of html.matchAll(/(?:href|src)="([^"]+)"/g)) {
      const h = m[1];
      if (/^(https?:|mailto:|#|data:)/.test(h)) continue;
      const [path] = h.split('#');
      if (!path) continue;
      let target = resolve(dirname(f), path.split('?')[0]);
      if (h.startsWith('/')) target = join(OUT, path);
      if (existsSync(target) && statSync(target).isDirectory()) target = join(target, 'index.html');
      if (!existsSync(target)) missing.push(`${f.slice(OUT.length)} -> ${h}`);
    }
  }
  assert.deepEqual(missing, []);
});

test('every page has a title, a description and a mobile viewport', () => {
  for (const f of htmlFiles) {
    const html = readFileSync(f, 'utf8');
    assert.match(html, /<meta name="viewport" content="width=device-width, initial-scale=1">/, f);
    assert.match(html, /<title>[^<]{3,}<\/title>/, f);
    assert.match(html, /<meta name="description" content="[^"]{10,}">/, f);
    assert.match(html, /<html lang="en">/, f);
  }
});

test('public text has no em dashes, banned words or private strings', () => {
  const banned = readFileSync(join(ROOT, 'e2e/banned-words.txt'), 'utf8').split('\n').map((l) => l.trim()).filter((l) => l && !l.startsWith('#'));
  const denyPath = join(ROOT, 'reference/denylist.txt');
  const deny = existsSync(denyPath) ? readFileSync(denyPath, 'utf8').split('\n').map((l) => l.trim().toLowerCase()).filter((l) => l && !l.startsWith('#')) : [];
  const sources = [...walk(join(SITE, 'src')), ...walk(join(SITE, 'data')), ...walk(join(SITE, 'public')), join(SITE, 'claims.json'), join(SITE, 'build.mjs'), ...['README.md', 'CONTRIBUTING.md', 'SECURITY.md', 'CODE_OF_CONDUCT.md'].map((f) => join(ROOT, f)).filter(existsSync)];
  const all = [...files.filter((f) => !f.endsWith('install.sh')), ...sources];
  for (const f of all) {
    const s = readFileSync(f, 'utf8');
    assert.ok(!s.includes('\u2014'), `em dash in ${f}`);
    for (const w of banned) assert.ok(!new RegExp(`\\b${w}\\b`, 'i').test(s), `"${w}" in ${f}`);
    const low = s.toLowerCase();
    for (const d of deny) assert.ok(!low.includes(d), `private string in ${f}`);
    assert.doesNotMatch(s, /\b(192\.168\.\d+\.\d+|172\.(1[6-9]|2\d|3[01])\.\d+\.\d+)\b/, `private address in ${f}`);
  }
});

test('source files carry an SPDX line', () => {
  for (const f of [...walk(join(SITE, 'src')).filter((x) => x.endsWith('.mjs')), join(SITE, 'build.mjs'), ...walk(join(SITE, 'scripts')), ...walk(join(SITE, 'test')), join(SITE, 'public/site.js'), join(SITE, 'public/deploy.js'), join(SITE, 'public/style.css')]) {
    assert.match(readFileSync(f, 'utf8').split('\n')[0], /SPDX-License-Identifier: MIT/, f);
  }
});

test('relative links work from any depth', () => {
  assert.equal(rel('', '/docs/quick-start'), 'docs/quick-start/');
  assert.equal(rel('docs/guides/volumes', '/docs/quick-start'), '../../quick-start/');
  assert.equal(rel('docs/guides/volumes', '/style.css'), '../../../style.css');
  assert.equal(rel('docs/quick-start', '/docs/quick-start#x'), './#x');
  assert.equal(rel('', '/'), './');
});

test('markdown renders tables, code, lists and escapes HTML', () => {
  const { html, toc } = render('## A b\n\n| x | y |\n|---|---|\n| `a\\|b` | <i> |\n\n```sh\necho <x>\n```\n\n1. one\n2. two\n   more\n\n> **Note:** hi', (h) => h);
  assert.match(html, /<h2 id="a-b">A b<\/h2>/);
  assert.match(html, /<td><code>a\|b<\/code><\/td><td>&lt;i&gt;<\/td>/);
  assert.match(html, /<code>echo &lt;x&gt;<\/code>/);
  assert.match(html, /<ol><li>one<\/li><li>two more<\/li><\/ol>/);
  assert.match(html, /class="callout note"/);
  assert.equal(toc[0].id, 'a-b');
  assert.equal(inline('**b** [l](/x) `c*d*`', (h) => h + '/'), '<strong>b</strong> <a href="/x/">l</a> <code>c*d*</code>');
});
