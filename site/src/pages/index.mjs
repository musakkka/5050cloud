// SPDX-License-Identifier: MIT
// The landing page (docs/SPEC.md 14.1, reference/design/site-rules.md). It is
// built around the real product: the hero is a recording of install, deploy,
// Postgres, a variable reference and a domain on a fresh VM (5050cloud-demo),
// and the console screenshots come from 5050cloud-brand/screens, both through
// scripts/media.mjs.
// Claims come from claims.json by id; prices from data/pricing.json; the
// all-cores figures from data/benchmark.json. Nothing here is drawn to look
// like the product.

import { escapeHtml as e } from '../lib/markdown.mjs';
import { REPO, INSTALL, INSTALL_WGET, INSTALL_GUIDED, INSTALL_CLI } from '../lib/config.mjs';
import { proven } from '../lib/claims.mjs';
import { shot, picture } from '../lib/media.mjs';

// keepTokens stops a command from breaking inside a short flag such as
// --token: each short token is kept whole, and long ones (URLs) may break only
// after a slash. The copied text is unchanged.
export function keepTokens(cmd) {
  const tok = (t) => (t.length <= 1 ? e(t) : `<span class="tok">${t.length <= 28 ? e(t) : e(t).replace(/\//g, '/<wbr>')}</span>`);
  return cmd.split('\n').map((line) => line.split(' ').map(tok).join(' ')).join('\n');
}

export function copyBlock(cmd, label, cls = '') {
  return `<div class="code cmd${cls ? ' ' + cls : ''}"><pre><code>${keepTokens(cmd)}</code></pre><button class="copy" type="button" aria-label="Copy ${e(label)}">Copy</button></div>`;
}

const money = (n) => `$${Number(n).toFixed(2).replace(/\.00$/, '')}`;

// demoSection is the full demo, shown only when the build found a finished,
// reviewed render in public/demo (build.mjs findDemo, scripts/media.mjs --demo).
// Without one the section is left out entirely: the page never shows a placeholder.
export function demoSection(r, demo) {
  if (!demo || !demo.ready) return '';
  const p = demo.poster;
  const posterSrc = r(`/demo/poster-${p.widths[p.widths.length - 1]}.webp`);
  const chapters = (demo.chapters || []).map((c) => `<li><button type="button" class="chapter" data-at="${c.start}"><span class="chapter-time">${e(c.time)}</span> ${e(c.text)}</button></li>`).join('');
  return `<section id="demo" class="demo" aria-labelledby="demo-h">
  <div class="wrap demo-grid">
    <div class="demo-head">
      <h2 id="demo-h">Watch the ${Math.round(demo.duration)} s demo</h2>
      <p>One take on a fresh VM: install, deploy, add Postgres, reference its URL, open the HTTPS address, scale. Captions are on; there is no sound.</p>
      ${chapters ? `<ol class="chapters" aria-label="Chapters">${chapters}</ol>` : ''}
    </div>
    <div class="mat demo-body">
      <video class="demo-video" controls muted playsinline preload="none" poster="${posterSrc}" width="${p.ratio[0]}" height="${p.ratio[1]}">
        <source src="${r('/demo/5050cloud-demo.mp4')}" type="video/mp4">
        <source src="${r('/demo/5050cloud-demo.webm')}" type="video/webm">
        <track kind="captions" src="${r('/demo/5050cloud-demo.en.vtt')}" srclang="en" label="English" default>
        ${demo.chapters ? `<track kind="chapters" src="${r('/demo/chapters.vtt')}" srclang="en" label="Chapters">` : ''}
      </video>
    </div>
  </div>
</section>`;
}

export function landing({ shell, rel, claims, pricing, bench, media, report, demo }) {
  const byId = new Map(claims.claims.map((c) => [c.id, c]));
  const ok = proven(claims, report);
  const get = (id) => {
    const x = byId.get(id);
    if (!x) throw new Error(`landing page uses claim ${id}, which is not in claims.json`);
    return x;
  };
  // say is the claim when its evidence holds (a passing report, a recording
  // or a source), else its fallback (the feature with no figure), else null.
  const say = (id) => {
    const x = get(id);
    if (ok.has(id)) return e(x.claim);
    if (x.fallback) return e(x.fallback);
    throw new Error(`landing page states claim ${id}, which has no passing evidence and no fallback`);
  };
  const raw = (id) => get(id).claim;
  const r = (h) => rel('', h);
  const ex = new Map(pricing.examples.map((x) => [x.id, x]));
  const rw = Object.fromEntries(pricing.prices.railway.map((p) => [`${p.item}|${p.unit}`, p]));
  const railPrice = (item, unit) => {
    const p = rw[`${item}|${unit}`];
    if (!p) throw new Error(`pricing.json has no Railway price ${item} ${unit}`);
    return p.value;
  };
  const steady = ex.get('api-steady');
  const steadyRail = steady.railway.find((x) => x.case === 'steady').monthly;
  const ded = steady.selfhost.find((x) => /^Hetzner CCX33/.test(x.option)).monthly + steady.addToEachSelfhost.backups;
  const typicalRail = steady.railway.find((x) => x.case === 'typical').monthly;
  const priceLine = `For the 4 vCPU API running all month: Railway ${money(steadyRail)} at steady load and ${money(typicalRail)} at typical load; a VM with 8 dedicated vCPU that fits it, about ${money(ded.toFixed(2))}.`;
  if (priceLine !== raw('price-steady')) throw new Error(`claims.json price-steady is out of date with pricing.json: ${priceLine}`);

  const full = '(min-width: 1800px) 1700px, (min-width: 721px) calc(100vw - 64px), 100vw';
  const half = '(min-width: 1800px) 1000px, (min-width: 1000px) 58vw, 100vw';

  // The hero: the real recording, with its steps beside it. Each step is a
  // button that jumps the recording to that step; site.js marks the step that
  // is playing. Without script the list is still the plain list of steps.
  // On a phone the whole screen is too small to read, so the hero shows the
  // capture's own Deploy a Docker image dialog instead, and the button plays
  // the recording at full width.
  const hv = media.videos['hero.mp4'];
  const hp = media.posters['hero-poster'];
  const chapters = media.hero.chapters;
  const steps = chapters
    .map((c) => `<li><button type="button" class="step" data-at="${c.start}" data-end="${c.end}"><span class="step-bar" aria-hidden="true"></span><span class="step-text">${e(c.text)}</span></button></li>`)
    .join('');
  const heroVideo = `<video class="hero-video" muted loop playsinline preload="metadata" poster="${r(`/media/hero-poster-${hp.widths[hp.widths.length - 1]}.webp`)}" width="${hv.w}" height="${hv.h}" aria-label="${e(`A recording of the real console: ${raw('hero-recording').toLowerCase()}`)}" data-autoplay>
<source src="${r('/media/hero.mp4')}" type="video/mp4">
<source src="${r('/media/hero.webm')}" type="video/webm">
<track kind="captions" src="${r('/media/hero.en.vtt')}" srclang="en" label="English">
<track kind="chapters" src="${r('/media/hero.chapters.vtt')}" srclang="en" label="Steps">
</video>`;
  const heroPhone = picture(r, media, 'hero-phone', { alt: 'The Deploy a Docker image dialog in the console, with nginx:1.27 typed in the Image field and the Deploy Now button', sizes: '100vw' });
  const transcript = chapters.map((c) => `<li><strong>${e(c.text)}.</strong> ${e(c.caption)}</li>`).join('');

  // The tour: one large real screenshot at a time, the whole window, with a
  // phone crop of the part that matters.
  const tour = [
    { id: 'canvas', tab: 'Canvas', shot: 'canvas', text: 'Every service of a project on one canvas. Dashed lines are variable references; changes wait in one bar until you deploy them.', alt: 'The canvas of the orbit project: web, api with 3 replicas, a cron job, a crashed worker, Postgres grouped with PgBouncer, Redis, docs with one staged change and search deploying, joined by dashed reference lines', doc: ['/docs/basics', 'The basics'] },
    { id: 'create', tab: 'Add menu', shot: 'palette', text: 'Add a GitHub repo, a database, a template, a Docker image, an empty service or a volume from one menu.', alt: 'The Add menu open on the canvas, listing GitHub Repo, Template, Database, Docker Image, Empty Service, Volume and Import Compose File', doc: ['/docs/guides/deploy-docker-image', 'Deploy a Docker image'] },
    { id: 'metrics', tab: 'Metrics', shot: 'metrics', text: 'CPU, memory, network, HTTP requests, error rate and response time for each service, with every deploy marked on the charts.', alt: 'The Metrics tab of the api service: CPU per replica over the last week with a deploy marker, and the top of the memory chart', doc: ['/docs/basics', 'The basics'] },
    { id: 'scale', tab: 'Replicas and all cores', shot: 'scale', text: 'Set Replicas and 5050cloud runs that many copies behind its proxy. Use all cores runs a Node service started with node server.js as one process per vCPU; other runtimes get WEB_CONCURRENCY.', alt: 'The Scale settings of the api service: CPU and memory limits per replica, the memory reservation, and Use all cores switched on under Processes', doc: ['/docs/guides/use-all-cores', 'Use all cores'] },
  ];
  const tabs = tour.map((t, i) => `<a class="tab" id="t-${t.id}" href="#p-${t.id}" role="tab" aria-controls="p-${t.id}" aria-selected="${i === 0}">${e(t.tab)}</a>`).join('');
  const benchLine = bench
    ? ` <span class="bench-line">${e(raw('bench'))} On an 8 vCPU VM with 4 physical cores, a CPU-bound Node service went from ${bench.rows.map((x) => `${x.off} to ${x.on} req/s`).join(' and from ')} (${bench.rows.map((x) => `${(x.on / x.off).toFixed(2)}x`).join(', ')}). ${e(bench.why)} <a href="${r('/docs/guides/use-all-cores')}">How it was measured</a></span>`
    : '';
  const panels = tour
    .map((t, i) => `<div class="tour-panel" id="p-${t.id}" role="tabpanel" aria-labelledby="t-${t.id}"${i ? ' data-hidden' : ''}><figure>
  <div class="shot-frame">${shot(r, media, t.shot, { alt: t.alt, sizes: full })}</div>
  <figcaption><span>${e(t.text)} <a href="${r(t.doc[0])}">${e(t.doc[1])}</a></span>${t.id === 'scale' ? `<span class="beta-note"><strong>Beta:</strong> replicas spread across several hosts work in 0.1.0 but are not yet covered by the end-to-end tests. <a href="${r('/docs/reference/honest-limits#beta')}">What is beta</a></span>${benchLine}` : ''}</figcaption>
</figure></div>`)
    .join('');

  // The price chart: one workload, four bars, every bar labelled with its price.
  const add = steady.addToEachSelfhost.backups;
  const pick = (re) => steady.selfhost.find((x) => re.test(x.option));
  const shared = pick(/^Hetzner CPX42/);
  const dedicated = pick(/^Hetzner CCX33/);
  const typical = steady.railway.find((x) => x.case === 'typical').monthly;
  const costRows = [
    { who: 'railway', name: 'Railway, steady load', note: 'all of it in use around the clock', monthly: steadyRail },
    { who: 'railway', name: 'Railway, typical load', note: 'about 1.6 vCPU and 6 GB on average', monthly: typical },
    { who: 'vm', name: 'Hetzner CCX33 with 5050cloud', note: '8 dedicated vCPU, 32 GB: the fair match for steady load', monthly: dedicated.monthly + add },
    { who: 'vm', name: 'Hetzner CPX42 with 5050cloud', note: '8 shared vCPU, 16 GB: may be throttled under constant load', monthly: shared.monthly + add },
  ];
  const costMax = Math.max(...costRows.map((x) => x.monthly));
  const costBars = costRows
    .map((x) => `<li class="bar is-${x.who}"><span class="bar-name">${e(x.name)}<small>${e(x.note)}</small></span><span class="bar-track"><span class="bar-fill" style="--w:${((x.monthly / costMax) * 100).toFixed(1)}%"></span><span class="bar-value">${money(x.monthly.toFixed(2))}</span></span></li>`)
    .join('');
  const sources = [...new Set([...pricing.prices.railway, ...(pricing.prices.hetzner || [])].map((p) => p.source))];

  const main = `<main id="main" class="landing">
<section class="hero" aria-labelledby="hero-h">
  <div class="wrap hero-grid">
    <div class="hero-text">
      <h1 id="hero-h">Railway's workflow on your own servers</h1>
      <p class="lead">Open source and self-hosted. Install it on a Linux VM and deploy apps and databases from a canvas, the way you do on Railway.</p>
      ${copyBlock(INSTALL, 'install command', 'hero-cmd')}
      <p class="hero-links"><a class="btn primary" href="${r('/docs/guides/install')}">Install on a VM</a> <a href="${REPO}" rel="noopener">View on GitHub</a></p>
    </div>
    <figure class="hero-demo">
      <div class="hero-screen">
        ${heroVideo}
        <div class="hero-phone">${heroPhone}</div>
        <button class="hero-toggle" type="button" hidden>Play</button>
      </div>
      <figcaption class="hero-cap">
        <ol class="steps" aria-label="Steps in the recording">${steps}</ol>
        <p class="hero-note">${e(raw('hero-recording'))}: the real console on a fresh 2 vCPU VM, ${e(media.hero.date)}, one take of ${Math.round(hv.duration)} s. Waits are sped up and labelled on screen; host names are replaced with example.com.</p>
        <details class="transcript"><summary>Read the steps</summary><ol>${transcript}</ol><p>The same flow, step by step, is the <a href="${r('/docs/quick-start')}">quick start</a>.</p></details>
      </figcaption>
    </figure>
  </div>
</section>

${demoSection(r, demo)}

<section id="console" class="tour" aria-labelledby="tour-h">
  <div class="wrap">
    <div class="tour-head">
      <h2 id="tour-h">The console you already know</h2>
      <div class="tabs" role="tablist" aria-label="Parts of the console">${tabs}</div>
    </div>
    ${panels}
  </div>
</section>

<section id="install" class="install" aria-labelledby="install-h">
  <div class="wrap install-grid">
    <figure class="install-shot">
      ${picture(r, media, 'terminal-install-poster', { alt: 'The installer’s real output on a fresh VM: packages, Docker Engine, the control plane, the console address and a one-time link to create the owner account', sizes: half })}
      <figcaption>What the installer prints on a fresh Ubuntu 24.04 VM. The address and token are masked.</figcaption>
    </figure>
    <div class="install-text">
      <h2 id="install-h">Install with one command</h2>
      <p>${say('install')}. Run it on a fresh Ubuntu 24.04 VM with a public IPv4 address and ports 80 and 443 open. It installs Docker, the control plane, the agent, Caddy for HTTPS and a private registry, then prints a one-time link to create your account.</p>
      <dl class="ways">
        <div><dt>With curl</dt><dd>${copyBlock(INSTALL, 'install command')}</dd></div>
        <div><dt>With wget</dt><dd>${copyBlock(INSTALL_WGET, 'wget install command')}</dd></div>
        <div><dt>Guided: asks for your domain and email</dt><dd>${copyBlock(INSTALL_GUIDED, 'guided install command')}</dd></div>
      </dl>
      <p class="install-foot">No domain needed: without one, the console and your services get sslip.io names with real certificates. See the <a href="${r('/docs/guides/install')}">install guide</a> and <a href="${r('/docs/reference/install-options')}">every install option</a>.</p>
    </div>
  </div>
</section>

<section id="domains" class="domains" aria-labelledby="domains-h">
  <div class="wrap domains-grid">
    <div class="domains-side">
      <h2 id="domains-h">Your domains, with HTTPS</h2>
      <p class="domains-lead">${say('domains')}.</p>
      <ol class="status">
        <li><strong>Generate Domain</strong> gives a public HTTPS name that works at once.</li>
        <li><strong>Custom Domain</strong> asks for one DNS record.</li>
        <li>The status moves from <strong>Waiting for DNS</strong> to <strong>Issuing certificate</strong> to <strong>Active</strong>.</li>
      </ol>
      <div class="table records">
        <table>
          <caption>Records for shop.example.com, host at 203.0.113.10. <a href="${r('/docs/guides/custom-domains')}">Custom domains guide</a></caption>
          <thead><tr><th scope="col">Type</th><th scope="col">Name</th><th scope="col">Value</th></tr></thead>
          <tbody>
            <tr><td>A</td><td><code>shop</code></td><td><code>203.0.113.10</code></td></tr>
            <tr><td>CNAME</td><td><code>www</code></td><td><code>shop.example.com</code></td></tr>
          </tbody>
        </table>
      </div>
    </div>
    <figure class="domains-shot">
      <div class="shot-frame">${shot(r, media, 'networking', { alt: 'The Networking settings of the web service: a generated domain and a custom domain, each with a valid certificate, the Generate Domain, Custom Domain and TCP Proxy buttons, and the private domain web.internal', sizes: half })}</div>
      <figcaption>Settings, Networking, on a service with a generated name and a custom domain.</figcaption>
    </figure>
  </div>
</section>

<section id="cost" class="cost field" aria-labelledby="cost-h">
  <div class="wrap cost-grid">
    <div class="cost-head">
      <h2 id="cost-h">What it costs</h2>
      <p class="cost-figure">${e(priceLine)}</p>
      <p>${e(raw('free'))} A small, mostly idle app costs about the same either way, and Railway also runs the servers for you.</p>
    </div>
    <figure class="cost-chart" id="w-api-steady">
      <figcaption class="chart-title">${e(steady.title)}. ${e(raw('cost-charts'))} VM prices include ${money(add)} for a backups bucket.</figcaption>
      <ul class="bars" aria-label="Monthly cost in US dollars">${costBars}</ul>
      <details class="assumptions"><summary>Prices and sources</summary>
        <ul>${steady.assumptions.map((a) => `<li>${e(a)}</li>`).join('')}</ul>
        <p>List prices in USD read on ${e(pricing.generated)}, excluding tax; euro prices at ${pricing.fx.usdPerEur} USD per EUR (ECB, ${e(pricing.fx.rateDate)}). Shared vCPUs under constant full load may be throttled, so the dedicated row is the fair comparison for the steady case. Check current prices: ${sources.map((s) => `<a href="${e(s)}" rel="noopener">${e(new URL(s).hostname.replace(/^www\./, '') + new URL(s).pathname.replace(/\/$/, ''))}</a>`).join(', ')}.</p>
      </details>
    </figure>
  </div>
</section>

<section id="railway" class="habits" aria-labelledby="habits-h">
  <div class="wrap">
    <h2 id="habits-h">Bring your Railway habits</h2>
    <p class="habits-lead">${say('alias')}. After <code>5050 alias install</code>, the commands you type today deploy to your own console.</p>
    <div class="habits-grid">
      <div class="table split-table stack-table habits-table">
        <table>
          <caption>Install the CLI on your laptop with <code class="nw">${e(INSTALL_CLI)}</code>. <a href="${r('/docs/coming-from-railway')}">Coming from Railway</a> lists every difference.</caption>
          <thead><tr><th scope="col">On Railway</th><th scope="col">On 5050cloud</th></tr></thead>
          <tbody>
            <tr><td data-label="On Railway"><code>railway up</code></td><td data-label="On 5050cloud"><code>railway up</code>, or <code>5050 up</code></td></tr>
            <tr><td data-label="On Railway"><code>railway link</code></td><td data-label="On 5050cloud"><code>railway link</code></td></tr>
            <tr><td data-label="On Railway"><code>\${{Postgres.DATABASE_URL}}</code></td><td data-label="On 5050cloud"><code>\${{Postgres.DATABASE_URL}}</code></td></tr>
            <tr><td data-label="On Railway"><code>railway.json</code> in the repo</td><td data-label="On 5050cloud">${say('railway-json')}</td></tr>
            <tr><td data-label="On Railway">Railpack builds</td><td data-label="On 5050cloud">Railpack builds, or your Dockerfile</td></tr>
            <tr><td data-label="On Railway">Import a project</td><td data-label="On 5050cloud">By hand in 0.1.0: <a href="${r('/docs/guides/import-from-railway')}">move a project</a></td></tr>
          </tbody>
        </table>
      </div>
      <figure class="habits-shot">
        <div class="shot-frame">${shot(r, media, 'variables', { alt: 'The Variables tab of the api service: DATABASE_URL open for editing with the value ${{Postgres.DATABASE_URL}}, then NODE_ENV, REDIS_URL, a sealed SESSION_SECRET and WEB_ORIGIN with hidden values', sizes: half })}</div>
        <figcaption>The same reference syntax in the 5050cloud console: <code>DATABASE_URL</code> follows Postgres wherever it runs. <a href="${r('/docs/guides/variables')}">Variables</a></figcaption>
      </figure>
    </div>
  </div>
</section>

<section id="limits" class="limits" aria-labelledby="limits-h">
  <div class="wrap">
    <h2 id="limits-h">Honest limits</h2>
    <div class="limit-group">
      <h3 id="beta">Beta in 0.1.0</h3>
      <div>
        <p class="small">These work but are not yet covered by the end-to-end tests that run on fresh VMs.</p>
        <ul class="plain">
          <li>More than one host: joining hosts, the private WireGuard network, a second ingress host.</li>
          <li>Replicas spread across hosts.</li>
          <li>Database backups to an S3-compatible bucket, and restoring them.</li>
          <li>Upgrades with <code>5050 upgrade</code> and their automatic revert.</li>
        </ul>
      </div>
    </div>
    <div class="limit-group">
      <h3>Not in 0.1.0</h3>
      <ul class="plain">
        <li>The Railway project importer. The <a href="${r('/docs/guides/import-from-railway')}">manual move</a> works today.</li>
        <li>PR environments, app sleeping, global regions and an edge network.</li>
        <li>Master key rotation, and enforcing two-factor login for everyone.</li>
        <li>A clear error when a certificate fails: it stays on <strong>Issuing certificate</strong>.</li>
      </ul>
    </div>
    <div class="limit-group">
      <h3>By design</h3>
      <ul class="plain">
        <li>One control host. If it is down, apps keep serving but deploys, cron and the console wait.</li>
        <li>Databases run on one host each, with no automatic failover.</li>
        <li>Services with a volume have a short gap on each deploy.</li>
        <li>You own the VM. Security updates are automatic; reboots are yours.</li>
      </ul>
    </div>
    <p class="limits-foot"><a href="${r('/docs/reference/honest-limits')}">Every limit, with what to do about it</a></p>
  </div>
</section>

<section id="open" class="open" aria-labelledby="open-h">
  <div class="wrap open-grid">
    <h2 id="open-h">Open source, no lock-in</h2>
    <p class="statement">The server is AGPL-3.0; the CLI, templates and installer are MIT. Every deployment is a standard Docker image, databases are ordinary containers, and <code>5050 config pull</code> writes your services to one file.</p>
    <ul class="open-links">
      <li><a href="${REPO}" rel="noopener">View on GitHub</a><span>The server, CLI, installer and templates in one repository.</span></li>
      <li><a href="${REPO}/releases/tag/v0.1.0" rel="noopener">Release v0.1.0</a><span>The server for Linux on amd64 and arm64, the CLI for Linux, macOS and Windows, and signed checksums.</span></li>
      <li><a href="${r('/docs/compare/railway')}">Compare with Railway, Coolify, Dokploy and Kamal</a><span>How they differ, feature by feature.</span></li>
    </ul>
  </div>
</section>

<section id="faq" class="faq" aria-labelledby="faq-h">
  <div class="wrap">
    <h2 id="faq-h">Questions</h2>
    <div class="faq-list">
      <details><summary>Is it cheaper than Railway for a small project?</summary><p>Usually not. Railway bills by use, so an idle app costs ${money(ex.get('small-app').railway[0].monthly)} to ${money(ex.get('small-app').railway[1].monthly)} a month there, about what a small VM costs. The saving shows up at steady load, with several apps on one VM, and with a lot of egress.</p></details>
      <details><summary>What do I need?</summary><p>A Linux VM with a public IPv4 address, ports 80 and 443 open, and root access. Ubuntu 24.04 is tested, on amd64 or arm64. 2 vCPU and 4 GB of RAM is a comfortable start.</p></details>
      <details><summary>Do I need a domain?</summary><p>No. Without one, the console and your services get sslip.io names with real certificates. Add your own domain at install time or later, per service.</p></details>
      <details><summary>Is it a fork of Railway?</summary><p>No. It is an independent project that follows Railway's ideas and names so your projects and habits move over. It is not affiliated with Railway. Builds use Railpack, Railway's open-source builder.</p></details>
      <details><summary>Can I use my Docker Compose file?</summary><p>Yes. <code>5050 import compose docker-compose.yml</code> stages its services, variables, ports and volumes for you to review and deploy.</p></details>
    </div>
  </div>
</section>

<section class="close" aria-labelledby="close-h">
  <div class="wrap close-inner">
    <h2 id="close-h">Install it on a VM you already pay for</h2>
    ${copyBlock(INSTALL, 'install command', 'close-cmd')}
    <p><a class="btn primary" href="${r('/docs/guides/install')}">Install on a VM</a> <a href="${r('/docs/quick-start')}">Read the quick start</a></p>
  </div>
</section>
</main>`;

  return shell({
    slug: '',
    title: "5050cloud: Railway's workflow on your own servers",
    description: 'Open source and self-hosted. Install 5050cloud on a Linux VM and deploy apps and databases from a canvas, the way you do on Railway. You pay only for the VM.',
    main,
    bodyClass: 'home',
    head: `<link rel="preload" as="image" href="${r('/media/hero-poster-1280.webp')}" media="(min-width: 721px)" fetchpriority="high">`,
  });
}

export { money };
