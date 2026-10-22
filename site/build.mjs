// SPDX-License-Identifier: MIT
// Builds the 5050cloud site into dist/: the landing page, the docs (HTML and
// a Markdown copy of every page), /deploy, /llms.txt, /llms-full.txt,
// /install.sh, a sitemap and a 404 page. No dependencies beyond Node itself.
//
//   node build.mjs [--out DIR]

import { readFileSync, writeFileSync, mkdirSync, rmSync, readdirSync, statSync, existsSync, copyFileSync } from 'node:fs';
import { join, dirname, relative, posix } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseFrontMatter, render, escapeHtml } from './src/lib/markdown.mjs';
import { apiPage, serviceSettingsPage, cliPages, templatePages } from './src/lib/generate.mjs';
import { landing } from './src/pages/index.mjs';
import { deployPage } from './src/pages/deploy.mjs';

const SITE = dirname(fileURLToPath(import.meta.url));
const ROOT = join(SITE, '..');
const ORIGIN = 'https://5050cloud.com';
const REPO = 'https://github.com/musakkka/5050cloud';

const outArg = process.argv.indexOf('--out');
const OUT = outArg > 0 ? process.argv[outArg + 1] : join(SITE, 'dist');

function walk(dir) {
  const files = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) files.push(...walk(p));
    else files.push(p);
  }
  return files;
}

// rel turns an absolute site path into one relative to the page at fromSlug,
// so the site works at the domain root and under a GitHub Pages project path.
export function rel(fromSlug, href) {
  if (!href.startsWith('/') || href.startsWith('//')) return href;
  const [pathPart, hash = ''] = href.split('#');
  const fromDir = fromSlug === '' ? '' : fromSlug;
  let target = pathPart.replace(/^\//, '');
  const isFile = /\.[a-z0-9]+$/i.test(target);
  if (!isFile && target !== '' && !target.endsWith('/')) target += '/';
  let r = posix.relative(fromDir || '.', target || '.');
  if (r === '') r = '.';
  if (!isFile && !r.endsWith('/')) r += '/';
  if (r === './') r = './';
  return r + (hash ? `#${hash}` : '');
}

function loadPages() {
  const pages = [];
  const docsDir = join(SITE, 'src/content/docs');
  for (const file of walk(docsDir)) {
    if (!file.endsWith('.md')) continue;
    const src = readFileSync(file, 'utf8');
    const { data, body } = parseFrontMatter(src);
    const slug = 'docs/' + relative(docsDir, file).replace(/\\/g, '/').replace(/\.md$/, '').replace(/\/index$/, '');
    if (!data.title || !data.description) throw new Error(`${file}: front matter needs title and description`);
    pages.push({ slug, title: data.title, description: data.description, body, source: relative(ROOT, file) });
  }
  const api = apiPage(ROOT);
  pages.push({ slug: 'docs/reference/api', title: 'API', description: 'The 5050cloud REST API serves everything the console and the CLI do, with the same roles and audit log.', body: api.md, source: 'pkg/api/routes.go' });
  const ss = serviceSettingsPage(ROOT);
  pages.push({ slug: 'docs/reference/service-settings', title: 'Service settings', description: 'Every setting a service has, with its default and the Railway setting it matches.', body: ss.md, source: 'pkg/api/types.go' });
  for (const c of cliPages()) pages.push({ slug: c.slug, title: c.title, description: c.description, body: c.md, source: 'site/data/cli.json' });
  const tpl = templatePages(ROOT);
  for (const t of tpl) pages.push({ slug: t.slug, title: t.title, description: t.description, body: t.md, source: `templates/${t.id}/template.yaml` });
  if (!sidebar.some((g) => g.label === 'Templates')) {
    const at = sidebar.findIndex((g) => g.label === 'CLI');
    sidebar.splice(at, 0, { label: 'Templates', items: tpl.map((t) => t.slug).sort() });
  }
  return pages;
}

const sidebar = JSON.parse(readFileSync(join(SITE, 'data/sidebar.json'), 'utf8'));

function sidebarHtml(pages, current) {
  const bySlug = new Map(pages.map((p) => [p.slug, p]));
  return sidebar
    .map((g) => {
      const items = g.items
        .map((slug) => {
          const p = bySlug.get(slug);
          if (!p) throw new Error(`sidebar lists ${slug}, which has no page`);
          const cur = slug === current ? ' aria-current="page"' : '';
          return `<li><a href="${rel(current, '/' + slug)}"${cur}>${escapeHtml(p.navTitle || p.title)}</a></li>`;
        })
        .join('');
      const open = g.items.includes(current) ? ' open' : '';
      return `<details${open}><summary>${escapeHtml(g.label)}</summary><ul>${items}</ul></details>`;
    })
    .join('');
}

export function shell({ slug, title, description, main, head = '', bodyClass = '' }) {
  const r = (h) => rel(slug, h);
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${escapeHtml(title)}</title>
<meta name="description" content="${escapeHtml(description)}">
<link rel="canonical" href="${ORIGIN}/${slug}">
<link rel="icon" href="${r('/favicon.svg')}" type="image/svg+xml">
<link rel="stylesheet" href="${r('/style.css')}">
${head}
</head>
<body class="${bodyClass}">
<a class="skip" href="#main">Skip to content</a>
<header class="top">
  <a class="brand" href="${r('/')}"><img src="${r('/favicon.svg')}" alt="" width="24" height="24"><span>5050cloud</span></a>
  <nav aria-label="Main">
    <a href="${r('/docs/quick-start')}">Docs</a>
    <a href="${r('/docs/cli/overview')}">CLI</a>
    <a href="${r('/docs/reference/api')}">API</a>
    <a href="${REPO}" rel="noopener">GitHub</a>
  </nav>
</header>
${main}
<footer class="foot">
  <p><a href="${REPO}" rel="noopener">GitHub</a> · <a href="${r('/docs/quick-start')}">Docs</a> · <a href="${REPO}/blob/main/LICENSING.md" rel="noopener">License: AGPL-3.0 server, MIT CLI</a> · <a href="${REPO}/blob/main/SECURITY.md" rel="noopener">Security policy</a> · <a href="${r('/llms.txt')}">llms.txt</a></p>
</footer>
<script src="${r('/site.js')}" defer></script>
</body>
</html>
`;
}

function docPage(page, pages) {
  const linkFn = (h) => rel(page.slug, h);
  const { html, toc } = render(page.body, linkFn);
  const tocHtml = toc.length > 2
    ? `<nav class="toc" aria-label="On this page"><p>On this page</p><ul>${toc.filter((t) => t.level === 2).map((t) => `<li><a href="#${t.id}">${escapeHtml(t.text)}</a></li>`).join('')}</ul></nav>`
    : '';
  const md = rel(page.slug, `/${page.slug}.md`);
  const main = `<div class="docs">
<button class="navtoggle" type="button" aria-expanded="false" aria-controls="sidebar">Menu</button>
<nav id="sidebar" class="sidebar" aria-label="Docs">${sidebarHtml(pages, page.slug)}</nav>
<main id="main" class="content">
<article>
<h1>${escapeHtml(page.title)}</h1>
${html}
</article>
<p class="meta"><a href="${md}">View as Markdown</a> · <a href="${REPO}/edit/main/${page.source}" rel="noopener">Edit this page</a></p>
</main>
${tocHtml}
</div>`;
  return shell({ slug: page.slug, title: `${page.title} | 5050cloud docs`, description: page.description, main });
}

function mdExport(page) {
  const abs = page.body.replace(/\]\((\/[^)\s]*)\)/g, `](${ORIGIN}$1)`);
  return `# ${page.title}\n\n> ${page.description}\n\n${abs.trim()}\n`;
}

function write(path, content) {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, content);
}

export function build(out = OUT) {
  rmSync(out, { recursive: true, force: true });
  mkdirSync(out, { recursive: true });
  const pages = loadPages();
  const slugs = new Set();
  for (const p of pages) {
    if (slugs.has(p.slug)) throw new Error(`duplicate page ${p.slug}`);
    slugs.add(p.slug);
  }
  for (const f of walk(join(SITE, 'public'))) {
    const dest = join(out, relative(join(SITE, 'public'), f));
    mkdirSync(dirname(dest), { recursive: true });
    copyFileSync(f, dest);
  }
  const installer = join(ROOT, 'install/install.sh');
  if (existsSync(installer)) copyFileSync(installer, join(out, 'install.sh'));
  else throw new Error('install/install.sh is missing; the site must serve /install.sh');

  for (const p of pages) {
    write(join(out, p.slug, 'index.html'), docPage(p, pages));
    write(join(out, `${p.slug}.md`), mdExport(p));
  }
  write(join(out, 'docs/index.html'), shell({ slug: 'docs', title: '5050cloud docs', description: 'Documentation for 5050cloud.', main: `<main id="main" class="content narrow"><h1>Documentation</h1><p>Start with the <a href="${rel('docs', '/docs/quick-start')}">Quick start</a>.</p>${sidebar.map((g) => `<h2>${escapeHtml(g.label)}</h2><ul>${g.items.map((s) => { const pg = pages.find((x) => x.slug === s); return `<li><a href="${rel('docs', '/' + s)}">${escapeHtml(pg.title)}</a>: ${escapeHtml(pg.description)}</li>`; }).join('')}</ul>`).join('')}</main>` }));

  const claims = JSON.parse(readFileSync(join(SITE, 'claims.json'), 'utf8'));
  const prices = JSON.parse(readFileSync(join(SITE, 'data/railway-prices.json'), 'utf8'));
  const costs = JSON.parse(readFileSync(join(SITE, 'data/cost-comparison.json'), 'utf8'));
  const reportPath = process.env.REPORT_JSON || join(SITE, 'data/report.json');
  const report = existsSync(reportPath) ? JSON.parse(readFileSync(reportPath, 'utf8')) : null;
  const screens = existsSync(join(SITE, 'public/screens')) ? readdirSync(join(SITE, 'public/screens')).filter((f) => /\.(png|webp|jpg)$/.test(f)).sort() : [];
  write(join(out, 'index.html'), landing({ shell, rel, claims, prices, costs, report, screens, REPO }));
  write(join(out, 'deploy/index.html'), deployPage({ shell, rel }));
  write(join(out, '404.html'), shell({ slug: '', title: 'Page not found | 5050cloud', description: 'This page does not exist.', main: '<main id="main" class="content narrow"><h1>Page not found</h1><p>This page does not exist. Try the <a href="/docs/quick-start/">Quick start</a> or the <a href="/">home page</a>.</p></main>' }));

  // llms.txt: an index, grouped like the sidebar. llms-full.txt: every page.
  const bySlug = new Map(pages.map((p) => [p.slug, p]));
  const lines = ['# 5050cloud', '', "> Railway's developer experience on servers you own. One command turns any Linux VM into a canvas of services with git deploys, databases, logs and rollbacks. Open source.", ''];
  for (const g of sidebar) {
    lines.push(`## ${g.label}`, '');
    for (const s of g.items) {
      const p = bySlug.get(s);
      lines.push(`- [${p.title}](${ORIGIN}/${s}.md): ${p.description}`);
    }
    lines.push('');
  }
  write(join(out, 'llms.txt'), lines.join('\n'));
  write(join(out, 'llms-full.txt'), sidebar.flatMap((g) => g.items.map((s) => mdExport(bySlug.get(s)))).join('\n---\n\n'));
  const urls = ['', 'deploy', 'docs', ...pages.map((p) => p.slug)];
  write(join(out, 'sitemap.xml'), `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${urls.map((u) => `<url><loc>${ORIGIN}/${u}${u ? '/' : ''}</loc></url>`).join('\n')}\n</urlset>\n`);
  write(join(out, 'robots.txt'), `User-agent: *\nAllow: /\nSitemap: ${ORIGIN}/sitemap.xml\n`);
  return { pages, out };
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  const { pages, out } = build();
  console.log(`Built ${pages.length} docs pages into ${relative(process.cwd(), out) || out}`);
}
