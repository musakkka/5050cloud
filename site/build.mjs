// SPDX-License-Identifier: MIT
// Builds the 5050cloud site into dist/: the landing page, the docs (HTML and
// a Markdown copy of every page), a search index, /deploy, /llms.txt,
// /llms-full.txt, /install.sh, a sitemap and a 404 page. No dependencies
// beyond Node itself.
//
//   node build.mjs [--out DIR]
//
// The site reads the 5050cloud source tree (pkg/api, templates, install) for
// the generated reference pages. It looks for it next to this folder (the
// site/ folder of a checkout), then in ../5050cloud, or wherever FIFTY_REPO
// points.

import { readFileSync, writeFileSync, mkdirSync, rmSync, readdirSync, statSync, existsSync, copyFileSync } from 'node:fs';
import { join, dirname, relative, posix, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseFrontMatter, render, escapeHtml, inline } from './src/lib/markdown.mjs';
import { apiPage, serviceSettingsPage, cliPages, templatePages } from './src/lib/generate.mjs';
import { ORIGIN, REPO, expand } from './src/lib/config.mjs';
import { landing } from './src/pages/index.mjs';
import { shot } from './src/lib/media.mjs';
import { deployPage } from './src/pages/deploy.mjs';

const SITE = dirname(fileURLToPath(import.meta.url));

export function findRoot() {
  const candidates = [process.env.FIFTY_REPO, join(SITE, '..'), join(SITE, '..', '5050cloud')].filter(Boolean);
  for (const c of candidates) if (existsSync(join(c, 'pkg/api/routes.go'))) return resolve(c);
  throw new Error(`cannot find the 5050cloud source tree (looked in ${candidates.join(', ')}). Set FIFTY_REPO.`);
}
export const ROOT = findRoot();

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

const sidebar = JSON.parse(readFileSync(join(SITE, 'data/sidebar.json'), 'utf8'));

function loadPages() {
  const pages = [];
  const docsDir = join(SITE, 'src/content/docs');
  for (const file of walk(docsDir)) {
    if (!file.endsWith('.md')) continue;
    const src = expand(readFileSync(file, 'utf8'), file);
    const { data, body } = parseFrontMatter(src);
    const slug = 'docs/' + relative(docsDir, file).replace(/\\/g, '/').replace(/\.md$/, '').replace(/\/index$/, '');
    if (!data.title || !data.description) throw new Error(`${file}: front matter needs title and description`);
    pages.push({ slug, title: data.title, navTitle: data.nav || '', description: data.description, body, source: `site/${relative(SITE, file).replace(/\\/g, '/')}` });
  }
  const api = apiPage(ROOT);
  pages.push({ slug: 'docs/reference/api', title: 'API', description: 'The 5050cloud REST API serves everything the console and the CLI do, with the same roles and audit log.', body: api.md, source: 'pkg/api/routes.go' });
  const ss = serviceSettingsPage(ROOT);
  pages.push({ slug: 'docs/reference/service-settings', title: 'Service settings', description: 'Every setting a service has, with its default and the Railway setting it matches.', body: ss.md, source: 'pkg/api/types.go' });
  for (const c of cliPages()) pages.push({ slug: c.slug, title: c.title, description: c.description, body: expand(c.md, c.slug), source: 'site/data/cli.json' });
  const tpl = templatePages(ROOT);
  for (const t of tpl) pages.push({ slug: t.slug, title: t.title, description: t.description, body: expand(t.md, t.slug), source: `templates/${t.id}/template.yaml` });
  if (!sidebar.some((g) => g.label === 'Templates')) {
    const at = sidebar.findIndex((g) => g.label === 'CLI');
    sidebar.splice(at, 0, { label: 'Templates', items: tpl.map((t) => t.slug).sort() });
  }
  return pages;
}

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
      const open = g.items.includes(current) || g.open ? ' open' : '';
      return `<details${open}><summary>${escapeHtml(g.label)}</summary><ul>${items}</ul></details>`;
    })
    .join('');
}

// The theme is applied before first paint so nothing flashes (K9).
const THEME_BOOT = `<script>(function(){var d=document.documentElement;d.classList.add('js');var t=null;try{t=localStorage.getItem('5050site.theme');}catch(e){}var m=/[?&]theme=(light|dark)/.exec(location.search);if(m)t=m[1];if(t==='light'||t==='dark')d.setAttribute('data-theme',t);})();</script>`;

// The 5050 logo, recoloured to the site: the first 50 takes the accent and the
// second takes the text colour, so it follows the light and dark themes.
const LOGO = `<svg class="logo" viewBox="95 45 850 345" role="img" aria-hidden="true" focusable="false"><g class="logo-a"><path d="M157 54C184 53 207 56 229 54C249 57 265 51 282 52C311 54 326 72 328 100C331 127 322 144 299 148L216 148C204 148 200 156 202 173C239 172 267 179 291 193C331 215 349 246 347 286C345 328 325 359 287 372C254 384 210 387 173 382C137 378 112 363 105 340C96 320 106 297 121 291C140 281 157 292 178 296C206 302 232 302 244 290C249 284 247 271 240 267C231 259 218 256 203 255L146 253C121 253 111 241 108 218C103 184 108 137 112 105C116 73 130 57 157 54Z" fill-rule="evenodd"/><path d="M405 98C463 98 507 129 529 182C552 234 544 292 516 332C490 369 453 379 410 374C352 371 308 341 290 292C272 244 283 185 312 145C336 114 366 97 405 98ZM405 181C379 181 364 200 365 226C365 256 382 277 405 278C433 280 449 257 450 232C451 204 432 182 405 181Z" fill-rule="evenodd"/></g><g class="logo-b"><path d="M568 54C594 54 610 56 634 55L670 55C699 54 713 68 714 97C715 124 705 140 681 142L599 142C585 142 582 149 583 162C586 168 604 166 621 169C680 171 714 202 724 250C738 312 711 358 658 372C630 380 587 380 558 374C525 369 506 356 499 335C493 318 501 293 518 287C538 280 563 290 584 289C609 289 627 280 629 264C630 248 615 242 592 242L552 241C520 240 508 228 506 201C504 168 508 130 511 106C514 72 533 55 568 54Z" fill-rule="evenodd"/><path d="M798 97C864 96 910 131 928 189C946 248 931 306 891 342C861 369 809 374 769 361C717 344 687 311 678 260C668 210 681 165 714 132C736 109 766 98 798 97ZM794 182C766 181 750 203 750 228C750 257 767 277 791 278C819 280 834 259 833 231C833 204 818 183 794 182Z" fill-rule="evenodd"/></g></svg>`;

const OG = existsSync(join(SITE, 'public/og/index.json')) ? JSON.parse(readFileSync(join(SITE, 'public/og/index.json'), 'utf8')) : {};
function ogImage(slug) {
  const key = slug === '' ? 'home' : slug.replace(/^docs\//, '');
  return OG[key] || OG.home || null;
}

export function shell({ slug, title, description, main, head = '', bodyClass = '', sheetExtra = '' }) {
  const r = (h) => rel(slug, h);
  const links = `<a href="${r('/docs')}"${slug.startsWith('docs') ? ' aria-current="true"' : ''}>Docs</a>
    <a href="${r('/#cost')}">Pricing</a>
    <a href="${r('/docs/compare/railway')}">Compare</a>
    <a href="${REPO}" rel="noopener">GitHub</a>`;
  const og = ogImage(slug);
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${escapeHtml(title)}</title>
<meta name="description" content="${escapeHtml(description)}">
<meta name="color-scheme" content="light dark">
<link rel="canonical" href="${ORIGIN}/${slug}${slug ? '/' : ''}">
<link rel="icon" href="${r('/favicon.ico')}" sizes="48x48">
<link rel="icon" href="${r('/favicon.svg')}" type="image/svg+xml">
<link rel="apple-touch-icon" href="${r('/apple-touch-icon.png')}">
<link rel="manifest" href="${r('/site.webmanifest')}">
<meta name="theme-color" content="#f5f5f2" media="(prefers-color-scheme: light)">
<meta name="theme-color" content="#111110" media="(prefers-color-scheme: dark)">
<meta property="og:type" content="website">
<meta property="og:site_name" content="5050cloud">
<meta property="og:title" content="${escapeHtml(title)}">
<meta property="og:description" content="${escapeHtml(description)}">
<meta property="og:url" content="${ORIGIN}/${slug}${slug ? '/' : ''}">
${og ? `<meta property="og:image" content="${ORIGIN}/og/${og}">
<meta property="og:image:width" content="1200">
<meta property="og:image:height" content="630">
<meta name="twitter:card" content="summary_large_image">` : ''}
<link rel="preload" href="${r('/fonts/archivo-400-600.woff2')}" as="font" type="font/woff2" crossorigin>
<link rel="stylesheet" href="${r('/style.css')}">
${THEME_BOOT}
${head}
</head>
<body class="${bodyClass}" data-root="${r('/')}">
<a class="skip" href="#main">Skip to content</a>
<header class="top">
  <a class="brand" href="${r('/')}" aria-label="5050cloud home">${LOGO}</a>
  <nav class="nav" aria-label="Main">
    ${links}
  </nav>
  <div class="tools">
    <button class="search-open" type="button" hidden><span>Search docs</span> <kbd>/</kbd></button>
    <button class="theme-toggle" type="button" hidden>Theme</button>
    <a class="nav-action" href="${r('/docs/guides/install')}">Install on a VM</a>
    <button class="menu-open" type="button" aria-expanded="false" aria-controls="sheet">Menu</button>
  </div>
</header>
<div class="sheet" id="sheet" hidden>
  <div class="sheet-head"><a class="brand" href="${r('/')}" aria-label="5050cloud home">${LOGO}</a><button class="menu-close" type="button">Close</button></div>
  <nav class="sheet-nav" aria-label="Main">${links}</nav>
  <p class="sheet-tools"><button class="search-open" type="button" hidden>Search docs</button> <button class="theme-toggle" type="button" hidden>Theme</button></p>
  ${sheetExtra}
</div>
${main}
<footer class="foot">
  <p class="foot-name"><a class="brand" href="${r('/')}" aria-label="5050cloud home">${LOGO}</a></p>
  <p class="foot-links"><a href="${REPO}" rel="noopener">GitHub</a> <a href="${r('/docs/quick-start')}">Docs</a> <a href="${REPO}/blob/main/LICENSING.md" rel="noopener">License</a> <a href="${REPO}/blob/main/SECURITY.md" rel="noopener">Security policy</a> <a href="${REPO}/releases/tag/v0.1.0" rel="noopener">Release v0.1.0</a> <a href="${r('/llms.txt')}">llms.txt</a></p>
  <p class="foot-note">AGPL-3.0 server, MIT CLI, API types, templates and installer. Not affiliated with Railway; Railway is a trademark of its owner.</p>
</footer>
<dialog class="search" id="search" aria-label="Search the docs">
  <form method="dialog" class="search-form" role="search">
    <label for="q">Search the docs</label>
    <input id="q" name="q" type="search" autocomplete="off" spellcheck="false" placeholder="custom domain, backups, railway alias">
    <button type="submit" class="search-close">Close</button>
  </form>
  <p class="search-status" aria-live="polite"></p>
  <ol class="search-results"></ol>
</dialog>
<p class="sr-only" aria-live="polite" id="announce"></p>
<script src="${r('/site.js')}" defer></script>
</body>
</html>
`;
}

export const MEDIA = JSON.parse(readFileSync(join(SITE, 'data/media.json'), 'utf8'));

// findDemo is the build-time check for the full demo: the section exists only
// when a finished render was copied into public/demo by
// `node scripts/media.mjs --demo <dir> --reviewed`. A missing or partial file
// leaves the section out; the page never shows a placeholder.
export function findDemo(dir = join(SITE, 'public/demo')) {
  const need = ['5050cloud-demo.mp4', '5050cloud-demo.webm', '5050cloud-demo.en.vtt', 'demo.json'];
  if (!need.every((f) => existsSync(join(dir, f)) && statSync(join(dir, f)).size > 0)) return { ready: false };
  // A finished MP4 has its index (moov) written; an encode still running does not.
  const mp4 = readFileSync(join(dir, '5050cloud-demo.mp4'));
  if (mp4.indexOf('moov') < 0) return { ready: false };
  const meta = JSON.parse(readFileSync(join(dir, 'demo.json'), 'utf8'));
  let chapters = null;
  if (existsSync(join(dir, 'chapters.vtt'))) {
    const vtt = readFileSync(join(dir, 'chapters.vtt'), 'utf8');
    chapters = [...vtt.matchAll(/(\d\d):(\d\d):(\d\d)\.(\d{3}) --> [^\n]+\n([^\n]+)/g)].map((m) => {
      const start = Number(m[1]) * 3600 + Number(m[2]) * 60 + Number(m[3]) + Number(m[4]) / 1000;
      return { start: start.toFixed(2), time: `${Math.floor(start / 60)}:${String(Math.floor(start % 60)).padStart(2, '0')}`, text: m[5].trim() };
    });
  }
  return { ready: true, duration: meta.duration, poster: meta.poster, chapters };
}

function flatOrder() {
  return sidebar.flatMap((g) => g.items);
}

function docPage(page, pages) {
  const linkFn = (h) => rel(page.slug, h);
  // A line `![alt](shot:networking "caption")` places a console screenshot,
  // light or dark with the theme.
  const shots = [];
  const body = page.body.replace(SHOT_LINE, (m, alt, name, cap1, cap2) => {
    const cap = cap1 || cap2;
    if (!MEDIA.stills[`${name}-light`]) throw new Error(`${page.slug}: no screenshot named ${name}`);
    shots.push({ alt, name, cap });
    return `SHOTMARKER${shots.length - 1}`;
  });
  const rendered = render(body, linkFn);
  const toc = rendered.toc;
  const html = rendered.html.replace(/<p>SHOTMARKER(\d+)<\/p>/g, (m, i) => {
    const x = shots[Number(i)];
    return `<figure class="doc-shot"><div class="shot">${shot(linkFn, MEDIA, x.name, { alt: x.alt, sizes: '(min-width: 1100px) 760px, calc(100vw - 32px)' })}</div>${x.cap ? `<figcaption>${escapeHtml(x.cap)}</figcaption>` : ''}</figure>`;
  });
  const tocItems = toc.filter((t) => t.level === 2);
  const tocHtml = tocItems.length > 2
    ? `<nav class="toc" aria-label="On this page"><p class="toc-title">On this page</p><ul>${tocItems.map((t) => `<li><a href="#${t.id}">${escapeHtml(t.text)}</a></li>`).join('')}</ul></nav>`
    : '<div class="toc" aria-hidden="true"></div>';
  const order = flatOrder();
  const i = order.indexOf(page.slug);
  const bySlug = new Map(pages.map((p) => [p.slug, p]));
  const prev = i > 0 ? bySlug.get(order[i - 1]) : null;
  const next = i >= 0 && i < order.length - 1 ? bySlug.get(order[i + 1]) : null;
  const pager = (prev || next)
    ? `<nav class="pager" aria-label="Previous and next">${prev ? `<a class="prev" href="${linkFn('/' + prev.slug)}"><span>Previous</span> ${escapeHtml(prev.navTitle || prev.title)}</a>` : '<span></span>'}${next ? `<a class="next" href="${linkFn('/' + next.slug)}"><span>Next</span> ${escapeHtml(next.navTitle || next.title)}</a>` : ''}</nav>`
    : '';
  const md = rel(page.slug, `/${page.slug}.md`);
  const side = sidebarHtml(pages, page.slug);
  const main = `<div class="docs">
<nav class="sidebar" aria-label="Docs">${side}</nav>
<main id="main" class="content">
<article>
<h1>${escapeHtml(page.title)}</h1>
${html}
</article>
${pager}
<p class="meta"><a href="${md}">View as Markdown</a> <a href="${REPO}/edit/main/${page.source}" rel="noopener">Edit this page</a></p>
</main>
${tocHtml}
</div>`;
  return shell({ slug: page.slug, title: `${page.title} | 5050cloud docs`, description: page.description, main, bodyClass: 'docs-page', sheetExtra: `<nav class="sheet-docs" aria-label="Docs">${side}</nav>` });
}

// Both `![alt](shot:name "caption")` and `![alt](shot:name) "caption"` are accepted.
const SHOT_LINE = /^!\[([^\]]+)\]\(shot:([a-z-]+)(?: "([^"]+)")?\)(?: "([^"]+)")?$/gm;

function mdExport(page) {
  const abs = page.body.replace(SHOT_LINE, (m, alt, name) => `![${alt}](/media/${name}-light-1600.png)`).replace(/\]\((\/[^)\s]*)\)/g, `](${ORIGIN}$1)`);
  return `# ${page.title}\n\n> ${page.description}\n\n${abs.trim()}\n`;
}

// searchIndex is what the docs search reads: title, path, description,
// headings and the start of the text of every page.
export function searchIndex(pages) {
  const plain = (md) => md
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/[`*_>#|]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
  return pages.map((p) => ({
    t: p.title,
    u: p.slug + '/',
    d: p.description,
    h: [...p.body.matchAll(/^#{2,3} (.+)$/gm)].map((m) => m[1].replace(/[`*]/g, '')),
    x: plain(p.body).slice(0, 700),
  }));
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
  const cliInstaller = join(ROOT, 'install/install-cli.sh');
  if (existsSync(cliInstaller)) copyFileSync(cliInstaller, join(out, 'install-cli.sh'));

  for (const p of pages) {
    write(join(out, p.slug, 'index.html'), docPage(p, pages));
    write(join(out, `${p.slug}.md`), mdExport(p));
  }
  write(join(out, 'search.json'), JSON.stringify(searchIndex(pages)));

  const bySlug = new Map(pages.map((p) => [p.slug, p]));
  const groupHtml = sidebar.map((g) => `<section class="docs-group"><h2>${escapeHtml(g.label)}</h2><ul>${g.items.map((sl) => { const pg = bySlug.get(sl); return `<li><a href="${rel('docs', '/' + sl)}">${escapeHtml(pg.navTitle || pg.title)}</a><span>${inline(pg.description, (h) => rel('docs', h))}</span></li>`; }).join('')}</ul></section>`).join('');
  const indexMain = `<main id="main" class="docs-home"><h1>Documentation</h1><p class="lead">Install 5050cloud on a VM, deploy your first service, and look up every setting, command and endpoint. Every page is also plain Markdown, listed in <a href="${rel('docs', '/llms.txt')}">llms.txt</a>.</p><p class="docs-home-start"><a class="btn primary" href="${rel('docs', '/docs/quick-start')}">Read the quick start</a> <a href="${rel('docs', '/docs/coming-from-railway')}">Coming from Railway</a> <a href="${rel('docs', '/docs/reference/honest-limits')}">Honest limits</a></p><div class="docs-groups">${groupHtml}</div></main>`;
  write(join(out, 'docs/index.html'), shell({ slug: 'docs', title: '5050cloud docs', description: 'Documentation for 5050cloud: install, set up, deploy, and reference for the CLI and API.', main: indexMain }));

  const claims = JSON.parse(readFileSync(join(SITE, 'claims.json'), 'utf8'));
  const pricing = JSON.parse(readFileSync(join(SITE, 'data/pricing.json'), 'utf8'));
  const benchPath = join(SITE, 'data/benchmark.json');
  const bench = existsSync(benchPath) ? JSON.parse(readFileSync(benchPath, 'utf8')) : null;
  const reportPath = process.env.REPORT_JSON || join(SITE, 'data/report.json');
  const report = existsSync(reportPath) ? JSON.parse(readFileSync(reportPath, 'utf8')) : null;
  write(join(out, 'index.html'), landing({ shell, rel, claims, pricing, bench, media: MEDIA, report, demo: findDemo() }));
  write(join(out, 'deploy/index.html'), deployPage({ shell, rel }));
  write(join(out, '404.html'), shell({ slug: '', title: 'Page not found | 5050cloud', description: 'This page does not exist on the 5050cloud site.', main: '<main id="main" class="content narrow"><h1>Page not found</h1><p>This page does not exist. Try the <a href="/docs/quick-start/">Quick start</a> or the <a href="/">home page</a>.</p></main>' }));

  // llms.txt: an index, grouped like the sidebar. llms-full.txt: every page.
  const lines = ['# 5050cloud', '', "> Railway's workflow on your own servers. Open source and self-hosted: one command turns any Linux VM into a canvas of services with git deploys, databases, logs and rollbacks. You pay only for the VM.", ''];
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
