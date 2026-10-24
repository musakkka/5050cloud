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
// Visitor counts: GoatCounter (no cookies, no personal data), on the live
// site only. GOATCOUNTER= (empty) turns it off; a code picks another account.
const GOATCOUNTER = process.env.GOATCOUNTER ?? '5050clooud';
const ANALYTICS = GOATCOUNTER
  ? `<script>if(/^(www\\.)?5050cloud\\.com$/.test(location.hostname)){var g=document.createElement('script');g.async=true;g.src='https://gc.zgo.at/count.js';g.dataset.goatcounter='https://${GOATCOUNTER}.goatcounter.com/count';document.head.appendChild(g);}</script>`
  : '';
const THEME_BOOT = `<script>(function(){var d=document.documentElement;d.classList.add('js');var t=null;try{t=localStorage.getItem('5050site.theme');}catch(e){}var m=/[?&]theme=(light|dark)/.exec(location.search);if(m)t=m[1];if(t==='light'||t==='dark')d.setAttribute('data-theme',t);})();</script>`;

// The 5050cloud lockup, as in the console (web/src/kit/Logo.tsx): one tile
// split into a pink half and an ink half, each holding "50", beside the
// wordmark. The tile keeps its colours in both themes; "5050" takes the text
// colour and "cloud" the muted one, so the wordmark follows the theme.
const LOGO = `<svg class="logo" viewBox="0 0 167.3 32" role="img" aria-hidden="true" focusable="false"><path d="M0 16h32v10a6 6 0 0 1-6 6H6a6 6 0 0 1-6-6z" fill="#13111c"/><rect class="logo-edge" x="0.5" y="0.5" width="31" height="31" rx="5.5" fill="none"/><path d="M0 6a6 6 0 0 1 6-6h20a6 6 0 0 1 6 6v10H0z" fill="#d5348a"/><path d="M12.49 11.9Q11.71 11.9 11.1 11.61Q10.49 11.33 10.13 10.82Q9.78 10.32 9.76 9.67H11.28Q11.29 9.96 11.46 10.19Q11.62 10.42 11.9 10.54Q12.17 10.67 12.49 10.67Q12.87 10.67 13.16 10.5Q13.46 10.33 13.62 10.02Q13.79 9.71 13.79 9.31Q13.79 8.91 13.62 8.59Q13.45 8.28 13.15 8.11Q12.85 7.93 12.46 7.93Q12.12 7.93 11.81 8.07Q11.49 8.2 11.32 8.43L9.92 8.19L10.28 4.2H14.88V5.49H11.57L11.38 7.41H11.42Q11.62 7.13 12.03 6.95Q12.43 6.78 12.92 6.78Q13.43 6.78 13.87 6.96Q14.3 7.14 14.62 7.48Q14.95 7.81 15.12 8.27Q15.3 8.73 15.3 9.28Q15.3 10.04 14.95 10.63Q14.59 11.22 13.96 11.56Q13.32 11.9 12.49 11.9ZM19.2 11.9Q18.24 11.9 17.56 11.44Q16.88 10.98 16.52 10.11Q16.16 9.24 16.16 8.01Q16.16 6.78 16.52 5.9Q16.88 5.03 17.56 4.56Q18.24 4.1 19.2 4.1Q20.16 4.1 20.84 4.56Q21.52 5.03 21.88 5.9Q22.24 6.78 22.24 8.01Q22.24 9.24 21.88 10.11Q21.52 10.98 20.84 11.44Q20.16 11.9 19.2 11.9ZM19.2 10.61Q19.67 10.61 19.99 10.31Q20.31 10 20.48 9.42Q20.66 8.84 20.66 8.01Q20.66 7.17 20.48 6.58Q20.31 6 19.99 5.69Q19.67 5.38 19.2 5.38Q18.73 5.38 18.41 5.69Q18.08 6 17.91 6.58Q17.74 7.17 17.74 8.01Q17.74 8.84 17.91 9.42Q18.08 10 18.41 10.31Q18.73 10.61 19.2 10.61Z" fill="#ffffff"/><path d="M12.49 11.9Q11.71 11.9 11.1 11.61Q10.49 11.33 10.13 10.82Q9.78 10.32 9.76 9.67H11.28Q11.29 9.96 11.46 10.19Q11.62 10.42 11.9 10.54Q12.17 10.67 12.49 10.67Q12.87 10.67 13.16 10.5Q13.46 10.33 13.62 10.02Q13.79 9.71 13.79 9.31Q13.79 8.91 13.62 8.59Q13.45 8.28 13.15 8.11Q12.85 7.93 12.46 7.93Q12.12 7.93 11.81 8.07Q11.49 8.2 11.32 8.43L9.92 8.19L10.28 4.2H14.88V5.49H11.57L11.38 7.41H11.42Q11.62 7.13 12.03 6.95Q12.43 6.78 12.92 6.78Q13.43 6.78 13.87 6.96Q14.3 7.14 14.62 7.48Q14.95 7.81 15.12 8.27Q15.3 8.73 15.3 9.28Q15.3 10.04 14.95 10.63Q14.59 11.22 13.96 11.56Q13.32 11.9 12.49 11.9ZM19.2 11.9Q18.24 11.9 17.56 11.44Q16.88 10.98 16.52 10.11Q16.16 9.24 16.16 8.01Q16.16 6.78 16.52 5.9Q16.88 5.03 17.56 4.56Q18.24 4.1 19.2 4.1Q20.16 4.1 20.84 4.56Q21.52 5.03 21.88 5.9Q22.24 6.78 22.24 8.01Q22.24 9.24 21.88 10.11Q21.52 10.98 20.84 11.44Q20.16 11.9 19.2 11.9ZM19.2 10.61Q19.67 10.61 19.99 10.31Q20.31 10 20.48 9.42Q20.66 8.84 20.66 8.01Q20.66 7.17 20.48 6.58Q20.31 6 19.99 5.69Q19.67 5.38 19.2 5.38Q18.73 5.38 18.41 5.69Q18.08 6 17.91 6.58Q17.74 7.17 17.74 8.01Q17.74 8.84 17.91 9.42Q18.08 10 18.41 10.31Q18.73 10.61 19.2 10.61Z" fill="#ea66ad" transform="translate(0 16)"/><path class="logo-ink" d="M49.39 25.64Q47.65 25.64 46.28 24.99Q44.91 24.34 44.11 23.19Q43.3 22.04 43.26 20.57H46.37Q46.4 21.3 46.82 21.85Q47.24 22.41 47.91 22.73Q48.58 23.04 49.39 23.04Q50.35 23.04 51.09 22.61Q51.82 22.17 52.25 21.4Q52.67 20.63 52.67 19.62Q52.67 18.61 52.23 17.82Q51.8 17.03 51.04 16.58Q50.28 16.13 49.3 16.13Q48.48 16.13 47.71 16.43Q46.94 16.74 46.49 17.23L43.59 16.76L44.52 7.58H54.84V10.25H47.17L46.67 14.99H46.76Q47.26 14.41 48.17 14.03Q49.07 13.65 50.15 13.65Q51.35 13.65 52.38 14.09Q53.41 14.53 54.17 15.32Q54.93 16.12 55.35 17.19Q55.77 18.27 55.77 19.55Q55.77 21.32 54.96 22.69Q54.14 24.06 52.71 24.85Q51.27 25.64 49.39 25.64ZM64.59 25.64Q62.43 25.64 60.9 24.56Q59.37 23.47 58.56 21.43Q57.74 19.39 57.74 16.5Q57.74 13.6 58.56 11.56Q59.38 9.51 60.91 8.42Q62.43 7.34 64.59 7.34Q66.75 7.34 68.28 8.42Q69.81 9.51 70.63 11.56Q71.44 13.6 71.44 16.5Q71.44 19.39 70.63 21.43Q69.83 23.47 68.29 24.56Q66.76 25.64 64.59 25.64ZM64.6 22.96Q65.76 22.96 66.57 22.2Q67.37 21.44 67.8 20Q68.22 18.56 68.22 16.5Q68.22 14.43 67.8 12.98Q67.37 11.54 66.57 10.77Q65.76 10 64.6 10Q63.43 10 62.62 10.77Q61.81 11.54 61.39 12.98Q60.96 14.43 60.96 16.5Q60.96 18.56 61.38 20Q61.8 21.44 62.61 22.2Q63.43 22.96 64.6 22.96ZM79.58 25.64Q77.83 25.64 76.46 24.99Q75.09 24.34 74.29 23.19Q73.49 22.04 73.44 20.57H76.55Q76.59 21.3 77 21.85Q77.42 22.41 78.09 22.73Q78.76 23.04 79.58 23.04Q80.53 23.04 81.27 22.61Q82 22.17 82.43 21.4Q82.85 20.63 82.85 19.62Q82.85 18.61 82.42 17.82Q81.98 17.03 81.22 16.58Q80.46 16.13 79.48 16.13Q78.67 16.13 77.9 16.43Q77.12 16.74 76.67 17.23L73.77 16.76L74.71 7.58H85.02V10.25H77.35L76.85 14.99H76.94Q77.45 14.41 78.35 14.03Q79.25 13.65 80.33 13.65Q81.54 13.65 82.57 14.09Q83.6 14.53 84.36 15.32Q85.12 16.12 85.53 17.19Q85.95 18.27 85.95 19.55Q85.95 21.32 85.14 22.69Q84.33 24.06 82.89 24.85Q81.45 25.64 79.58 25.64ZM94.77 25.64Q92.62 25.64 91.09 24.56Q89.55 23.47 88.74 21.43Q87.93 19.39 87.93 16.5Q87.93 13.6 88.75 11.56Q89.57 9.51 91.09 8.42Q92.62 7.34 94.77 7.34Q96.94 7.34 98.47 8.42Q100 9.51 100.81 11.56Q101.62 13.6 101.62 16.5Q101.62 19.39 100.82 21.43Q100.01 23.47 98.48 24.56Q96.95 25.64 94.77 25.64ZM94.78 22.96Q95.94 22.96 96.75 22.2Q97.56 21.44 97.98 20Q98.41 18.56 98.41 16.5Q98.41 14.43 97.98 12.98Q97.56 11.54 96.75 10.77Q95.94 10 94.78 10Q93.61 10 92.8 10.77Q91.99 11.54 91.57 12.98Q91.14 14.43 91.14 16.5Q91.14 18.56 91.56 20Q91.98 21.44 92.8 22.2Q93.61 22.96 94.78 22.96Z"/><path class="logo-cloud" d="M109.82 25.68Q107.87 25.68 106.43 24.81Q105 23.94 104.21 22.39Q103.43 20.84 103.43 18.78Q103.43 16.7 104.21 15.14Q105 13.58 106.43 12.72Q107.87 11.86 109.82 11.86Q110.93 11.86 111.89 12.15Q112.85 12.43 113.6 12.98Q114.35 13.52 114.86 14.3Q115.37 15.08 115.59 16.06L112.68 16.67Q112.55 16.15 112.3 15.73Q112.06 15.3 111.7 15Q111.34 14.71 110.87 14.54Q110.41 14.38 109.84 14.38Q108.75 14.38 108.04 14.96Q107.32 15.54 106.96 16.53Q106.6 17.52 106.6 18.77Q106.6 20 106.96 21Q107.32 21.99 108.04 22.57Q108.75 23.15 109.84 23.15Q110.42 23.15 110.89 22.98Q111.36 22.82 111.73 22.5Q112.09 22.19 112.34 21.75Q112.59 21.31 112.71 20.77L115.62 21.38Q115.41 22.39 114.9 23.17Q114.39 23.96 113.63 24.52Q112.87 25.08 111.91 25.38Q110.94 25.68 109.82 25.68ZM120.91 7.58V25.4H117.8V7.58ZM129.52 25.68Q127.57 25.68 126.14 24.81Q124.7 23.94 123.92 22.39Q123.14 20.84 123.14 18.78Q123.14 16.7 123.92 15.14Q124.7 13.58 126.14 12.72Q127.57 11.86 129.52 11.86Q131.49 11.86 132.92 12.72Q134.36 13.58 135.14 15.14Q135.92 16.7 135.92 18.78Q135.92 20.84 135.14 22.39Q134.36 23.94 132.92 24.81Q131.49 25.68 129.52 25.68ZM129.52 23.15Q130.61 23.15 131.34 22.56Q132.06 21.98 132.41 20.99Q132.77 19.99 132.77 18.77Q132.77 17.54 132.41 16.55Q132.06 15.55 131.34 14.97Q130.61 14.38 129.52 14.38Q128.43 14.38 127.72 14.97Q127.01 15.55 126.66 16.54Q126.31 17.53 126.31 18.77Q126.31 19.99 126.66 20.99Q127.01 21.98 127.72 22.56Q128.43 23.15 129.52 23.15ZM142.73 25.57Q141.36 25.57 140.32 24.98Q139.29 24.4 138.72 23.26Q138.15 22.13 138.15 20.53V12.03H141.26V20.05Q141.26 21.39 141.96 22.16Q142.66 22.92 143.88 22.92Q144.7 22.92 145.35 22.56Q146 22.21 146.36 21.51Q146.73 20.82 146.73 19.86V12.03H149.85V25.4H146.89L146.87 22.73Q146.33 24.01 145.43 24.71Q144.33 25.57 142.73 25.57ZM157.59 25.64Q156.01 25.64 154.76 24.83Q153.51 24.02 152.79 22.48Q152.07 20.94 152.07 18.74Q152.07 16.51 152.81 14.97Q153.55 13.44 154.8 12.65Q156.05 11.86 157.58 11.86Q158.77 11.86 159.53 12.26Q160.29 12.66 160.74 13.21Q161.19 13.76 161.42 14.24H161.55V7.58H164.66V25.4H161.61V23.28H161.42Q161.18 23.76 160.72 24.31Q160.26 24.86 159.5 25.25Q158.75 25.64 157.59 25.64ZM158.44 23.08Q159.46 23.08 160.16 22.53Q160.87 21.99 161.24 21.01Q161.61 20.03 161.61 18.72Q161.61 17.42 161.24 16.46Q160.88 15.49 160.17 14.96Q159.46 14.42 158.44 14.42Q157.39 14.42 156.68 14.97Q155.97 15.53 155.62 16.51Q155.26 17.48 155.26 18.72Q155.26 19.97 155.62 20.96Q155.97 21.94 156.69 22.51Q157.4 23.08 158.44 23.08Z"/></svg>`;

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
<meta name="theme-color" content="#f6f6f8" media="(prefers-color-scheme: light)">
<meta name="theme-color" content="#13111c" media="(prefers-color-scheme: dark)">
<meta property="og:type" content="website">
<meta property="og:site_name" content="5050cloud">
<meta property="og:title" content="${escapeHtml(title)}">
<meta property="og:description" content="${escapeHtml(description)}">
<meta property="og:url" content="${ORIGIN}/${slug}${slug ? '/' : ''}">
${og ? `<meta property="og:image" content="${ORIGIN}/og/${og}">
<meta property="og:image:width" content="1200">
<meta property="og:image:height" content="630">
<meta name="twitter:card" content="summary_large_image">` : ''}
<link rel="preload" href="${r('/fonts/inter-latin-wght.woff2')}" as="font" type="font/woff2" crossorigin>
<link rel="preload" href="${r('/fonts/ibm-plex-serif-latin-500.woff2')}" as="font" type="font/woff2" crossorigin>
<link rel="stylesheet" href="${r('/style.css')}">
${THEME_BOOT}
${ANALYTICS}
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
