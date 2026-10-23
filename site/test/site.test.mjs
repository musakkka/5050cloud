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
import { build, rel, ROOT, searchIndex } from '../build.mjs';
import { demoSection, landing, copyBlock } from '../src/pages/index.mjs';
import { INSTALL, INSTALL_WGET, INSTALL_GUIDED, INSTALL_CLI, expand } from '../src/lib/config.mjs';
import { parseRoutes } from '../src/lib/generate.mjs';
import { checkShape, checkClaims, proven, fallbackOk } from '../src/lib/claims.mjs';
import { shell, MEDIA, findDemo } from '../build.mjs';
import { render, inline } from '../src/lib/markdown.mjs';

const SITE = resolve(dirname(fileURLToPath(import.meta.url)), '..');
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
const textOf = (html) => html.replace(/<wbr>/g, '').replace(/<script[\s\S]*?<\/script>/g, '').replace(/<[^>]+>/g, ' ').replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/\s+/g, ' ');

// The pages section 14.2 requires, as slugs.
const SETUP = ['install', 'console-domain', 'firewall', 'smtp', 'github-app', 'backups-bucket', 'add-a-host', 'behind-cloudflare', 'cli-setup', 'members-roles-2fa', 'upgrading', 'restore'];
const REQUIRED = [
  'quick-start', 'basics', 'coming-from-railway', 'production-checklist',
  ...SETUP.map((s) => `guides/${s}`),
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

test('landing sections follow section 14.1 and the site rules', () => {
  const html = read('index.html');
  const text = textOf(html);
  assert.ok(text.includes("Railway's workflow on your own servers"));
  assert.equal((html.match(/<h1[ >]/g) || []).length, 1, 'one h1');
  for (const cmd of [INSTALL, INSTALL_WGET, INSTALL_GUIDED, INSTALL_CLI]) assert.ok(text.includes(cmd), cmd);
  assert.equal(INSTALL, 'curl -fsSL https://5050cloud.com/install.sh | sudo sh');
  for (const h of ['The console you already know', 'Install with one command', 'Your domains, with HTTPS', 'What it costs', 'Bring your Railway habits', 'Honest limits', 'Open source, no lock-in', 'Questions', 'Install it on a VM you already pay for']) {
    assert.ok(html.includes(`>${h}</h2>`), h);
  }
  assert.ok(html.indexOf('id="cost-h"') < html.indexOf('id="limits-h"'));
  assert.ok(/check current prices/i.test(text));
  assert.ok(text.includes('2026-10-22'), 'comparison is dated');
  assert.doesNotMatch(text, /cheaper for everyone|github stars|\bstars\b|get started|learn more|save up to|\d+x faster/i);
  // The comparison chart: one workload, Railway and VM bars with their prices as text.
  assert.match(html, /<figure class="cost-chart" id="w-api-steady">/);
  assert.equal((html.match(/<li class="bar /g) || []).length, 4, 'four bars, one chart');
  assert.match(html, /<li class="bar is-railway">[\s\S]*?\$220\.15/);
  assert.match(html, /<li class="bar is-vm">[\s\S]*?\$79\.38/);
  assert.match(html, /<li class="bar is-vm">[\s\S]*?\$156\.89/);
  // Beta is labelled where the landing page mentions it, and listed under Honest limits.
  assert.match(text, /Beta: replicas spread across several hosts/);
  for (const b of ['More than one host', 'Replicas spread across hosts', 'Database backups to an S3-compatible bucket', 'Upgrades with 5050 upgrade', 'The Railway project importer']) assert.ok(text.includes(b), `limits list ${b}`);
});

test('the landing page is built around real product media', () => {
  const html = read('index.html');
  const imgs = [...html.matchAll(/<img [^>]*>/g)].map((m) => m[0]);
  assert.ok(imgs.length >= 10, `only ${imgs.length} pictures`);
  for (const img of imgs) {
    assert.match(img, /alt="[^"]{20,}"/, img);
    assert.match(img, /width="\d+" height="\d+"/, img);
    assert.match(img, /src="media\/[a-z-]+-\d+\.png"/, img);
    assert.match(img, /loading="lazy"/, `below the fold, so lazy: ${img}`);
  }
  // Every still ships as AVIF and WebP with a PNG fallback, in both themes.
  assert.equal((html.match(/<source type="image\/avif"/g) || []).length, imgs.length);
  assert.equal((html.match(/<source type="image\/webp"/g) || []).length, imgs.length);
  for (const n of ['canvas', 'variables', 'metrics', 'palette', 'scale', 'networking']) {
    assert.ok(html.includes(`media/${n}-light-800.avif`) && html.includes(`media/${n}-dark-800.avif`), n);
    // A phone crop of the part that matters, served below 720 px (site rules L9).
    assert.ok(html.includes(`media/${n}-light-m-720.avif`), `${n} phone crop`);
  }
  // The worker in the screenshot project prints sample job lines, so its log
  // picture stays off the landing page (G5).
  assert.doesNotMatch(html, /media\/logs-/);
  // The hero's poster and phone picture are real, readable console frames, and
  // the recording has a captions track, a steps track and a transcript.
  assert.match(html, /poster="media\/hero-poster-1600\.webp"/);
  assert.match(html, /media\/hero-phone-720\.avif/);
  assert.match(html, /<track kind="captions" src="media\/hero\.en\.vtt"/);
  assert.match(html, /<track kind="chapters" src="media\/hero\.chapters\.vtt"/);
  assert.match(html, /<details class="transcript"><summary>Read the steps<\/summary>/);
  for (const f of ['media/hero.en.vtt', 'media/hero.chapters.vtt']) assert.match(readFileSync(join(OUT, f), 'utf8'), /^WEBVTT\n/, f);
  // The wordmark is text, not an invented logo mark (K1).
  assert.match(html, /<span class="wordmark">5050cloud<\/span>/);
  assert.doesNotMatch(html, /class="logo"/);
  // The hero recording: muted, looping, inline, with a poster and both formats,
  // and its steps (from the render's chapters) beside it as buttons.
  const videos = [...html.matchAll(/<video [^>]*>[\s\S]*?<\/video>/g)].map((m) => m[0]);
  assert.equal(videos.length, 1);
  assert.ok((html.match(/<button type="button" class="step" data-at="/g) || []).length >= 4, 'hero steps');
  for (const v of videos) {
    for (const want of ['muted', 'loop', 'playsinline', 'poster="media/', 'type="video/webm"', 'type="video/mp4"', 'aria-label="', '<track kind="captions"']) assert.ok(v.includes(want), want);
    assert.doesNotMatch(v, / autoplay/, 'site.js starts loops only without reduced motion');
  }
  for (const f of ['media/hero.mp4', 'media/hero.webm']) {
    assert.ok(existsSync(join(OUT, f)), f);
    assert.ok(statSync(join(OUT, f)).size < 4 * 1024 * 1024, `${f} is small`);
  }
  // No fake browser chrome around the recording (site rules I3).
  assert.doesNotMatch(html, /frame-bar|class="frame"/);
  assert.doesNotMatch(html, /\d+-\d+-\d+-\d+\.sslip\.io/);
});

test('the demo section appears only for a finished render, never as a placeholder', () => {
  const r = (h) => h;
  assert.equal(demoSection(r, { ready: false }), '');
  assert.equal(demoSection(r, null), '');
  const ready = { ready: true, duration: 90.9, poster: { widths: [960, 1920], ratio: [1920, 1080] }, chapters: [{ start: '3.00', time: '0:03', text: 'Install' }] };
  const video = demoSection(r, ready);
  for (const want of ['Watch the 91 s demo', '<video', 'controls', 'playsinline', 'muted', 'poster="/demo/poster-1920.webp"', 'src="/demo/5050cloud-demo.webm"', 'src="/demo/5050cloud-demo.mp4"', '<track kind="captions" src="/demo/5050cloud-demo.en.vtt"', '<track kind="chapters"', 'data-at="3.00"']) assert.ok(video.includes(want), want);
  assert.doesNotMatch(video, /autoplay/);
  // The build checks for the files: an empty folder means no section.
  assert.equal(findDemo(mkdtempSync(join(tmpdir(), '5050demo-'))).ready, false);
  const html = read('index.html');
  if (findDemo().ready) assert.match(html, /id="demo"/);
  else assert.doesNotMatch(html, /id="demo"|Watch the \d+ s demo|5050cloud-demo\./);
});

test('every setup guide opens with its description and ends with a success check and next steps', () => {
  for (const s of SETUP) {
    const md = read(`docs/guides/${s}.md`);
    const desc = md.split('\n')[2].replace(/^> /, '');
    const body = md.split('\n').slice(4).join('\n');
    assert.ok(body.trim().replace(/`/g, '').startsWith(desc.replace(/`/g, '')), `${s} opens with its description`);
    assert.match(md, /## What success looks like/, `${s} success check`);
    assert.match(md, /## Next steps/, `${s} next steps`);
  }
  const sidebar = JSON.parse(readFileSync(join(SITE, 'data/sidebar.json'), 'utf8'));
  assert.deepEqual(sidebar[1].items, SETUP.map((s) => `docs/guides/${s}`));
});

test('the install command is the same on the landing page, the quick start and the install guide', () => {
  for (const f of ['index.html', 'docs/quick-start/index.html', 'docs/guides/install/index.html']) {
    assert.ok(textOf(read(f)).includes(INSTALL), f);
  }
  // Every install command anywhere uses 5050cloud.com/install.sh and nothing else.
  for (const f of files.filter((x) => /\.(html|md|txt)$/.test(x))) {
    const t = readFileSync(f, 'utf8').replace(/<wbr>/g, '').replace(/<[^>]+>/g, '');
    assert.ok(!t.includes('get.5050cloud.com'), `unresolvable install host in ${f}`);
    for (const m of t.matchAll(/(?:curl|wget)\s+-[a-zA-Z]+\s+(https?:\/\/\S+)/g)) {
      if (/install/.test(m[1])) assert.ok(m[1].startsWith('https://5050cloud.com/install.sh'), `${f}: ${m[0]}`);
    }
  }
});

test('the custom domains guide documents the full job and links to the one Not yet list', () => {
  const html = read('docs/guides/custom-domains/index.html');
  for (const want of ['Waiting for DNS', 'Issuing certificate', 'Active', 'Retry certificate', 'CNAME', 'CAA', 'AAAA', 'Cloudflare', '5050 domain certificate retry']) assert.ok(html.includes(want), want);
  assert.match(html, /honest-limits\/#not-yet/);
  for (const f of htmlFiles) assert.doesNotMatch(readFileSync(f, 'utf8').replace(/<script[\s\S]*?<\/script>/g, ''), /coming in 0\.1\.1|arrives with release|until 0\.1\.1/i, f);
});

test('docs pages have search, a sidebar, an on-page list, and prev and next links', () => {
  const html = read('docs/guides/install/index.html');
  assert.match(html, /<dialog class="search"/);
  assert.match(html, /<nav class="sidebar" aria-label="Docs">/);
  assert.match(html, /aria-current="page"/);
  assert.match(html, /<nav class="toc" aria-label="On this page">/);
  assert.match(html, /<nav class="pager"/);
  const idx = JSON.parse(read('search.json'));
  assert.equal(idx.length, pages.length);
  const hit = idx.find((p) => p.u === 'docs/guides/custom-domains/');
  assert.ok(hit && hit.h.includes('Watch the status'));
  for (const p of idx) assert.ok(existsSync(join(OUT, p.u, 'index.html')), p.u);
});

test('placeholders expand and unknown ones fail the build', () => {
  assert.equal(expand('{{INSTALL}}'), INSTALL);
  assert.equal(expand('${{Postgres.DATABASE_URL}}'), '${{Postgres.DATABASE_URL}}');
  assert.throws(() => expand('{{NOPE}}'));
  for (const f of files.filter((x) => /\.(html|md|txt|json)$/.test(x))) assert.doesNotMatch(readFileSync(f, 'utf8'), /(?<!\$)\{\{[A-Z_]+\}\}/, f);
});

test('every landing claim is listed in claims.json with evidence and a condition', () => {
  const claims = JSON.parse(readFileSync(join(SITE, 'claims.json'), 'utf8'));
  let known = null;
  if (SPEC) known = new Set([...SPEC.matchAll(/^\| (E\d+[a-z]?) \|/gm)].map((m) => m[1]));
  assert.deepEqual(checkShape(claims, known), []);
  const text = textOf(read('index.html'));
  // No report ships with the site, so only source-backed claims are stated; test-backed ones show their fallback or nothing.
  const ok = proven(claims, null);
  for (const c of claims.claims) {
    if (ok.has(c.id)) assert.ok(text.includes(c.claim), `claim ${c.id} is on the page`);
    else if (c.claim !== c.fallback) assert.ok(!text.includes(c.claim), `unproven claim ${c.id} is not on the page`);
    if (c.fallback) assert.ok(fallbackOk(c.fallback), `fallback of ${c.id}`);
  }
  for (const n of ['under 5 minutes', 'under 2 minutes', 'Zero-downtime', '1.6x', '120 s', 'Reliable by default']) assert.ok(!read('index.html').includes(n), `${n} needs a passing report`);
});

test('with a passing report the tested wording appears, without one the fallback does', () => {
  const claims = JSON.parse(readFileSync(join(SITE, 'claims.json'), 'utf8'));
  const tests = [...new Set(claims.claims.flatMap((c) => c.evidence).filter((e) => !e.startsWith('source:') && !/^(recording|bench|data):/.test(e)))].map((id) => ({ id, pass: true }));
  const data = (f) => JSON.parse(readFileSync(join(SITE, 'data', f), 'utf8'));
  const args = { shell, rel, claims, pricing: data('pricing.json'), bench: data('benchmark.json'), media: MEDIA, demo: { ready: false } };
  const withReport = textOf(landing({ ...args, report: { tests } }));
  const without = textOf(landing({ ...args, report: null }));
  for (const id of ['install', 'domains', 'alias', 'railway-json']) {
    const c = claims.claims.find((x) => x.id === id);
    assert.ok(withReport.includes(c.claim), `${id} with a report`);
    assert.ok(without.includes(c.fallback), `${id} fallback without a report`);
    if (c.claim !== c.fallback) assert.ok(!without.includes(c.claim), `${id} claim needs a report`);
  }
});

test('the prices and the cores figure come from dated data and say only what ships', () => {
  const pricing = JSON.parse(readFileSync(join(SITE, 'data/pricing.json'), 'utf8'));
  assert.match(pricing.generated, /^\d{4}-\d\d-\d\d$/);
  assert.doesNotMatch(JSON.stringify(pricing.examples), /standby|no single host/i);
  const bench = JSON.parse(readFileSync(join(SITE, 'data/benchmark.json'), 'utf8'));
  const html = textOf(read('index.html'));
  for (const row of bench.rows) assert.ok(html.includes(`${(row.on / row.off).toFixed(2)}x`), row.work);
  assert.ok(html.includes(bench.why), 'the reason for the small gain is stated');
  assert.doesNotMatch(html, /1\.6x/);
});

test('commands do not break inside a flag, and the join command carries no roles', () => {
  const html = copyBlock('curl -fsSL https://5050cloud.com/install.sh | sudo sh -s -- --join https://console.example.com --token J-...', 'x');
  assert.match(html, /<span class="tok">--token<\/span>/);
  assert.ok(textOf(html).includes('--join https://console.example.com --token J-...'));
  assert.doesNotMatch(read('index.html'), /--token J-\.\.\.[^<]*<\/span>? --roles|--roles<\/span> <span class="tok">app/);
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
  const all = [...files.filter((f) => !f.endsWith('install.sh')), ...sources].filter((f) => /\.(html|md|txt|json|js|mjs|css|svg|vtt|xml)$/.test(f));
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
  assert.match(html, /<td data-label="x"><code class="nw">a\|b<\/code><\/td><td data-label="y">&lt;i&gt;<\/td>/);
  assert.match(html, /<div class="table stack-table">/);
  assert.match(html, /<code>echo &lt;x&gt;<\/code>/);
  assert.match(html, /<ol><li>one<\/li><li>two more<\/li><\/ol>/);
  assert.match(html, /class="callout note"/);
  assert.equal(toc[0].id, 'a-b');
  assert.equal(inline('**b** [l](/x) `c*d*`', (h) => h + '/'), '<strong>b</strong> <a href="/x/">l</a> <code>c*d*</code>');
});
