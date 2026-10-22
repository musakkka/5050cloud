// SPDX-License-Identifier: MIT
// The landing page (docs/SPEC.md 14.1). Every claim is pulled from
// claims.json by id, so the page and the evidence list cannot drift apart.

import { escapeHtml as e } from '../lib/markdown.mjs';

const INSTALL = 'curl -fsSL https://5050cloud.com/install.sh | sudo sh';
const INSTALL_WGET = 'wget -qO- https://5050cloud.com/install.sh | sudo sh';
const JOIN = 'curl -fsSL https://5050cloud.com/install.sh | sudo sh -s -- --join https://console.example.com --token J-... --roles app';

function copyBlock(cmd, label) {
  return `<div class="code cmd"><pre><code>${e(cmd)}</code></pre><button class="copy" type="button" aria-label="Copy ${e(label)}">Copy</button></div>`;
}

// e20 reads the measured result from a report.json entry when one is given.
function e20Result(report) {
  if (!report) return null;
  const tests = Array.isArray(report) ? report : report.tests || report.results || [];
  const t = tests.find((x) => (x.id || x.test) === 'E20');
  if (!t || !(t.pass === true || t.status === 'pass' || t.result === 'pass')) return null;
  const ev = t.evidence || {};
  return { ratio: ev.ratio, single: ev.rpsSingle || ev.rps1, auto: ev.rpsAuto, sku: t.vmSku || t.sku || report.vmSku || ev.sku };
}

export function landing({ shell, rel, claims, costs, prices, report, screens, REPO }) {
  const byId = new Map(claims.claims.map((c) => [c.id, c]));
  const c = (id) => {
    const x = byId.get(id);
    if (!x) throw new Error(`landing page uses claim ${id}, which is not in claims.json`);
    return e(x.claim);
  };
  const r = (h) => rel('', h);
  const e20 = e20Result(report);

  const screensHtml = screens.length
    ? `<section id="screens"><h2>See it</h2><div class="screens">${screens
        .map((f) => `<figure><img src="${r('/screens/' + f)}" alt="${e(f.replace(/[-_]/g, ' ').replace(/\.\w+$/, ''))}" loading="lazy" width="1280" height="800"></figure>`)
        .join('')}</div></section>`
    : '';

  const costRows = costs.rows
    .map((row) => `<tr><td>${e(row.option)}</td><td>${e(row.monthly)}</td><td>${e(row.vsRailway)}</td><td><a href="${e(row.source)}" rel="noopener">source</a></td></tr>`)
    .join('');

  const e20Html = e20
    ? `<p class="big">${e(String(e20.single))} requests per second with 1 process, ${e(String(e20.auto))} with Processes set to auto (${e(String(e20.ratio))}x) on ${e(String(e20.sku))}.</p>`
    : `<p class="big">${c('cores')}.</p>`;

  const main = `<main id="main" class="landing">
<section class="hero">
  <h1>Railway's developer experience on servers you own.</h1>
  <p class="sub">${c('install')} with git deploys, databases, logs and rollbacks. Open source. No per-seat, per-minute or per-server fees.</p>
  ${copyBlock(INSTALL, 'install command')}
  <p class="small">Or with wget: <code>${e(INSTALL_WGET)}</code>. Ubuntu 24.04 is the tested OS. Piped, it asks nothing; in a terminal it offers a guided setup. Every answer is also a flag. See <a href="${r('/docs/reference/install-options')}">Install options</a>.</p>
  <p class="actions"><a class="btn primary" href="${r('/docs/quick-start')}">Get started</a><a class="btn" href="${REPO}" rel="noopener">GitHub</a></p>
  <p class="small">${c('first-deploy')}.</p>
  <p class="small">v0.1.0 is an early release: see <a href="#limits">the limits below</a> for what has been tested so far.</p>
</section>

${screensHtml}

<section id="cost">
  <h2>Why it costs less</h2>
  <p>The workload: ${e(costs.workload)}</p>
  <div class="table"><table>
    <thead><tr><th>Option</th><th>Monthly</th><th>vs Railway steady ($${e(String(costs.railwaySteady))})</th><th>Price page</th></tr></thead>
    <tbody>${costRows}</tbody>
  </table></div>
  <p class="small">List prices read on ${e(costs.date)}; check current prices. Exchange rate: ${e(costs.exchangeRate)}. 5050cloud itself costs $0. Railway list prices used: $${prices.cpuPerVcpuMonth} per vCPU month, $${prices.memoryPerGbMonth} per GB of RAM month, $${prices.egressPerGb} per GB egress, $${prices.volumePerGbMonth} per GB of volume month (<a href="${e(prices.source)}" rel="noopener">source</a>, ${e(prices.date)}).</p>
  <ul>
    <li><strong>Low average use favours Railway.</strong> At typical load (an average of 1 vCPU and 8 GB) Railway costs about $${e(String(costs.railwayTypical))}, about the same as a Hetzner CCX23 and less than a DigitalOcean, AWS on-demand or Azure VM. For idle hobby projects there is no saving to claim.</li>
    <li><strong>Egress is included</strong> on Hetzner in the EU (20 TB) and on DigitalOcean (5 to 6 TB).</li>
    <li><strong>Growth is flat on a VM.</strong> Twice the traffic costs nothing until the box is full; the next step is a fixed price you choose.</li>
    <li><strong>You own patching.</strong> The installer turns on unattended security updates and <code>5050 upgrade</code> reverts itself when the new version is unhealthy. Reboots are yours.</li>
  </ul>
  <p>See your own number: the <strong>Usage</strong> page shows what your use would cost on Railway.</p>
  <p class="small">Headline: ${c('price-steady')}.</p>
</section>

<section id="cores">
  <h2>Use every core you pay for</h2>
  ${e20Html}
  <p>Most Node apps run one process and use one core. Turn on <strong>Use all cores</strong> (Settings > Scale) and 5050cloud runs one process per vCPU. For a start command of the form <code>node server.js</code> it runs the processes in Node cluster mode sharing one port; for every other runtime it sets <code>WEB_CONCURRENCY</code>, which gunicorn, uvicorn, puma and many frameworks read. Test condition: ${e(byId.get('cores').condition)} <a href="${REPO}/tree/main/examples" rel="noopener">The test app and script</a> are in the repo.</p>
  <p><a href="${r('/docs/guides/use-all-cores')}">Use all cores guide</a></p>
</section>

<section id="hosts">
  <h2>Add servers with one command</h2>
  ${copyBlock(JOIN, 'join command')}
  <ul>
    <li>${c('join')}. Get the command from <strong>Hosts > Add host</strong> or <code>5050 host add</code>.</li>
    <li>${c('no-inbound')}.</li>
    <li>${c('replicas')}.</li>
    <li>${c('mesh')}.</li>
    <li>${c('second-ingress')}.</li>
  </ul>
</section>

<section id="reliable">
  <h2>Reliable by default</h2>
  <ul>
    <li>${c('backups')}.</li>
    <li>${c('zero-downtime')}.</li>
    <li>${c('failed-deploy')}.</li>
    <li>${c('rollback')}.</li>
    <li>${c('upgrade')}.</li>
    <li>${c('cp-down')}.</li>
    <li>${c('state-stream')}.</li>
  </ul>
  <p class="small">v0.1.0 is an early release. A fresh single-host install was smoke tested end to end (install, sign-up, image deploy, generated HTTPS domain, Postgres with a variable reference). The full release suite on fresh VMs, which proves each line above, runs next. The test conditions are listed in <a href="${REPO}/blob/main/site/claims.json" rel="noopener">claims.json</a>.</p>
</section>

<section id="move">
  <h2>Move from Railway in minutes</h2>
  <ul>
    <li>${c('import')}.</li>
    <li>${c('railway-json')}.</li>
    <li>${c('alias')}: <code>\${{Postgres.DATABASE_URL}}</code> works as it does on Railway.</li>
  </ul>
  <p><a href="${r('/docs/coming-from-railway')}">Coming from Railway</a> lists every difference.</p>
</section>

<section id="lockin">
  <h2>No lock-in</h2>
  <ul>
    <li>Builds use Railpack, Railway's own builder, or your Dockerfile.</li>
    <li>Every deployment is a standard Docker image.</li>
    <li><code>5050 config pull</code> exports an environment's services and settings to one file.</li>
    <li>The server is AGPL-3.0. The CLI, API types, templates and installer are MIT.</li>
  </ul>
</section>

<section id="compare">
  <h2>Compared with</h2>
  <div class="table"><table>
    <thead><tr><th></th><th>5050cloud</th><th>Railway</th><th>Coolify</th><th>Dokploy</th><th>Kamal</th><th>Dokku</th></tr></thead>
    <tbody>
      <tr><td>Runs on your servers</td><td>Yes</td><td>No (managed)</td><td>Yes</td><td>Yes</td><td>Yes</td><td>Yes</td></tr>
      <tr><td>Web UI</td><td>Yes, a canvas</td><td>Yes, a canvas</td><td>Yes</td><td>Yes</td><td>No</td><td>No (paid Pro)</td></tr>
      <tr><td>Variable references between services</td><td>Yes, Railway syntax</td><td>Yes</td><td>No</td><td>No</td><td>No</td><td>No</td></tr>
      <tr><td>Multi-host</td><td>Yes, built-in load balancing</td><td>Managed regions</td><td>One copy per server, bring a load balancer</td><td>Swarm cluster, needs a registry</td><td>Needs an external load balancer</td><td>K3s scheduler</td></tr>
      <tr><td>Replicas on one host</td><td>Yes</td><td>Yes</td><td>No</td><td>Yes</td><td>One container per role per host</td><td>Yes</td></tr>
      <tr><td>Zero-downtime deploys</td><td>Yes, without a volume</td><td>Yes</td><td>Rolling update, with exceptions</td><td>Swarm rolling updates</td><td>Yes</td><td>Yes</td></tr>
      <tr><td>Backups with restore</td><td>Point-in-time Postgres, restore to a copy</td><td>Yes</td><td>Scheduled dumps, restore largely manual</td><td>DB and volume backups</td><td>None built in</td><td>Plugins</td></tr>
      <tr><td>Inbound management port</td><td>None (agents dial out)</td><td>Not applicable</td><td>SSH</td><td>Swarm ports</td><td>SSH</td><td>SSH</td></tr>
      <tr><td>License</td><td>AGPL-3.0 server, MIT CLI</td><td>Proprietary</td><td>Apache-2.0</td><td>Apache-2.0 plus a proprietary directory</td><td>MIT</td><td>MIT</td></tr>
    </tbody>
  </table></div>
  <p class="small">Facts as of 2026-10-22, from each project's docs. Details per tool under <a href="${r('/docs/compare/coolify')}">Compare</a>.</p>
</section>

<section id="limits">
  <h2>Honest limits</h2>
  <ul>
    <li>v0.1.0 is an early release. A fresh single-host install on Ubuntu 24.04 was smoke tested: sign-up, an image deploy, a generated HTTPS domain and Postgres with a variable reference. Multi-host, backups, upgrades and the Railway import are built but have not yet been through the full release test suite.</li>
    <li>One control host. With one host, a host failure stops everything until it is rebuilt. With more hosts, apps on other hosts keep serving (with a second ingress host), but deploys, cron and the console stop until the control host is back or rebuilt.</li>
    <li>Cron runs pause while the control plane is down; missed runs are listed, not replayed.</li>
    <li>Databases run on one host each. No high availability, no automatic failover.</li>
    <li>Services with a volume have a short gap on every deploy.</li>
    <li>Without a second ingress host, all public traffic enters through one machine. No global edge, CDN or DDoS protection.</li>
    <li>No autoscaling. Volumes have no size cap; they share the host disk.</li>
    <li>With a bucket, control plane state is replicated within seconds; without one, the recovery point is the last hourly snapshot on the same disk.</li>
    <li>Metrics are kept 30 days. Logs are kept 7 days on each host.</li>
    <li>IPv4 only. Ubuntu 24.04 is the tested OS.</li>
    <li>You own the hardware and the OS (security updates are automated, reboots are yours).</li>
  </ul>
  <p><a href="${r('/docs/reference/honest-limits')}">Details for each limit</a></p>
</section>

<section id="faq">
  <h2>FAQ</h2>
  <details><summary>What does it cost?</summary><p>Nothing. 5050cloud is open source. You pay your server provider and, if you want off-site backups, a few dollars for an S3-compatible bucket.</p></details>
  <details><summary>What do I need?</summary><p>A Linux VM with a public IPv4 address, ports 80 and 443 open, and root access. Ubuntu 24.04 is tested; Ubuntu 22.04 and Debian 12 install with a warning. 2 vCPU and 4 GB of RAM is a comfortable start.</p></details>
  <details><summary>Do I need a domain?</summary><p>No. Without one, services get <code>sslip.io</code> names with real certificates. Add your own domain at install time with <code>--domain</code> or later per service.</p></details>
  <details><summary>Is it a fork of Railway?</summary><p>No. It is an independent project that follows Railway's ideas and names so your projects and habits move over. It is not affiliated with Railway.</p></details>
  <details><summary>Can I use my existing Docker Compose file?</summary><p>Yes. <code>5050 import compose docker-compose.yml</code> or drag it onto the canvas. Services, variables, ports, volumes and dependencies are staged for you to review.</p></details>
  <details><summary>How do I get my data out?</summary><p>Your databases are standard containers you can dump with <code>5050 connect</code>, backups sit in your own bucket, and <code>5050 config pull</code> writes your services to a file.</p></details>
</section>
</main>`;

  return shell({
    slug: '',
    title: "5050cloud: Railway's developer experience on servers you own",
    description: 'One command turns any Linux VM into a canvas of services with git deploys, databases, logs and rollbacks. Open source. No per-seat, per-minute or per-server fees.',
    main,
    bodyClass: 'home',
  });
}
