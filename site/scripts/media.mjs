// SPDX-License-Identifier: MIT
// Makes the site's pictures and recordings from the brand repo's real console
// captures (5050cloud-brand/screens). Run it after the captures change; its
// output in public/media and data/media.json is committed, so `npm run build`
// stays dependency-free.
//
//   node scripts/media.mjs [--brand ../5050cloud-brand]
//   node scripts/media.mjs --stills        (console stills only)
//   node scripts/media.mjs --hero-only     (the hero loop, its poster and phone still only)
//   node scripts/media.mjs --demo ../5050cloud-demo/out --reviewed
//
// Needs ffmpeg with libsvtav1 (AVIF) and the Chromium that the brand repo's
// playwright-core installs (WebP, because this ffmpeg has no WebP encoder).
//
// Every still is written three ways: AVIF and WebP for browsers that take
// them, PNG as the fallback. Widths are listed per picture in data/media.json.
//
// The demo video is copied only with --reviewed: the person running it has
// watched the whole render and confirmed it shows no private hostname, IP,
// sslip.io name, token or account name. The site shows the demo section only
// when the files are in public/demo (build.mjs checks).

import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, copyFileSync, writeFileSync, readFileSync, rmSync, statSync, readdirSync } from 'node:fs';
import { join, dirname, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { tmpdir } from 'node:os';

const SITE = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const arg = (name, dflt) => {
  const i = process.argv.indexOf(name);
  return i > 0 ? process.argv[i + 1] : dflt;
};
const BRAND = resolve(arg('--brand', join(SITE, '..', '5050cloud-brand')));
// The hero loop: a HERO=1 render from the demo pipeline (5050cloud-demo:
// scripts/hero-frames.mjs, then HERO=1 node render.mjs --takes work/takes-hero
// --out work/hero). It is install, deploy an image, add Postgres, reference its
// URL, generate a domain and open it, recorded on a fresh VM in the console's
// dark theme (its pink accent is the site's accent), with the sslip.io names
// replaced by example.com names at capture time (HERO=1 node capture.mjs).
const HERO = resolve(arg('--hero', join(SITE, '..', '5050cloud-demo', 'work', 'hero')));
// The loop starts at the first console step: the install command is printed
// as text right beside it, so the recording does not open on a terminal. The
// poster is a frame from the middle of the loop (seconds into the encoded
// video), at the moment both services are deployed on the whole canvas, so the
// project name and the sidebar are in view and the frame is not cropped. The
// loop itself opens on an empty canvas, which is a poor first impression.
const HERO_FROM = arg('--hero-from', 'image');
const HERO_POSTER_AT = Number(arg('--hero-poster-at', '9.9'));
// The phone picture: the canvas Add menu's Docker image picker with nginx:1.27
// typed, from the capture itself (3200 x 1800), cropped to the picker so its
// text stays readable at 390 px. The crop is 880 px wide, so no width is larger.
const HERO_PHONE = { take: join(HERO, '..', 'takes-hero', 'deploy', 'frames', '000110.jpg'), crop: { x: 1160, y: 200, w: 880, h: 660 }, widths: [720, 880] };
const SCREENS = join(BRAND, 'screens');
const OUT = join(SITE, 'public/media');

// Crops are in the capture's own pixels (2880 x 1800, a 1440 x 900 window at
// 2x). Each one ends at a clean boundary: a whole field, a whole row, the edge
// of the window, never through a line of text. `phone` is a second crop of the
// part that matters, at 4:3, served below 720 px so the text stays readable.
const H = (x, y, w, h) => ({ x: x * 2, y: y * 2, w: w * 2, h: h * 2 });
const STILLS = [
  // The canvas: the whole window; the phone crop keeps web, api and the cron job.
  { name: 'canvas', crop: null, phone: H(190, 110, 560, 420), widths: [800, 1600, 2400] },
  // The Add menu open over the canvas; the phone crop is the menu.
  { name: 'palette', crop: null, phone: H(960, 110, 480, 360), widths: [800, 1600, 2400] },
  // Metrics: the whole window; the phone crop is the CPU chart.
  { name: 'metrics', crop: null, phone: H(572, 316, 560, 420), widths: [800, 1600, 2400] },
  // The project log explorer; only the log filters reference page uses it.
  { name: 'logs', crop: null, widths: [800, 1600] },
  // Deployments: the service panel from its title to the last history row.
  { name: 'deployments', crop: H(528, 82, 912, 768), widths: [800, 1600] },
  // Variables: the panel from its title to the provided variables line.
  { name: 'variables', crop: H(528, 82, 912, 520), phone: H(566, 252, 644, 483), widths: [800, 1600] },
  // Networking: the section heading to the private domain.
  { name: 'networking', crop: H(580, 300, 620, 520), phone: H(600, 330, 600, 450), widths: [800, 1600] },
  // Scale: the whole window; the phone crop keeps the limits and Use all cores.
  { name: 'scale', crop: null, phone: H(590, 380, 640, 480), widths: [800, 1600, 2400] },
];
// The terminal recordings are cropped to the terminal's text: the capture's
// own window bar and the tinted backdrop around it are left out.
const TERM = { x: 66, y: 86, w: 1148, h: 586 };
const POSTERS = [
  // The phone crop keeps the end of the run: ready, the setup link and the CLI line.
  { name: 'terminal-install-poster', src: 'terminal-install-poster.png', widths: [574, 1148], crop: TERM, phone: { x: 76, y: 496, w: 714, h: 136 } },
  { name: 'terminal-up-poster', src: 'terminal-up-poster.png', widths: [574, 1148], crop: TERM },
];
const VIDEOS = [
  { file: 'terminal-install.mp4', crop: TERM }, { file: 'terminal-install.webm', crop: TERM },
  { file: 'terminal-up.mp4', crop: TERM }, { file: 'terminal-up.webm', crop: TERM },
];

const ff = (...a) => execFileSync('ffmpeg', ['-v', 'error', '-y', ...a], { stdio: ['ignore', 'ignore', 'inherit'], env: { ...process.env, SVT_LOG: '1' } });
const probe = (f) => {
  const s = execFileSync('ffprobe', ['-v', 'error', '-show_entries', 'stream=width,height:format=duration', '-of', 'json', f], { encoding: 'utf8' });
  const j = JSON.parse(s);
  return { w: j.streams[0].width, h: j.streams[0].height, duration: Number(j.format.duration) };
};

async function webp(pngs) {
  const pw = await import(pathToFileURL(join(BRAND, 'node_modules/playwright-core/index.mjs')).href);
  const browser = await pw.chromium.launch();
  const page = await browser.newPage();
  for (const [png, out] of pngs) {
    const b64 = readFileSync(png).toString('base64');
    const data = await page.evaluate(async (src) => {
      const img = new Image();
      img.src = src;
      await img.decode();
      const c = document.createElement('canvas');
      c.width = img.naturalWidth;
      c.height = img.naturalHeight;
      c.getContext('2d').drawImage(img, 0, 0);
      return c.toDataURL('image/webp', 0.86);
    }, `data:image/png;base64,${b64}`);
    writeFileSync(out, Buffer.from(data.split(',')[1], 'base64'));
  }
  await browser.close();
}

// still writes name-<w>.{avif,webp,png} for each width.
function still(src, name, crop, widths, toWebp) {
  const files = [];
  for (const w of widths) {
    const vf = [crop ? `crop=${crop.w}:${crop.h}:${crop.x}:${crop.y}` : null, `scale=${w}:-2:flags=lanczos`].filter(Boolean).join(',');
    const png = join(OUT, `${name}-${w}.png`);
    ff('-i', src, '-vf', vf, '-frames:v', '1', png);
    ff('-i', png, '-pix_fmt', 'yuv420p', '-c:v', 'libsvtav1', '-crf', '28', '-preset', '6', '-frames:v', '1', '-svtav1-params', 'still-picture=1', join(OUT, `${name}-${w}.avif`));
    toWebp.push([png, join(OUT, `${name}-${w}.webp`)]);
    files.push(w);
  }
  const { w, h } = probe(join(OUT, `${name}-${widths[widths.length - 1]}.png`));
  return { widths: files, ratio: [w, h] };
}

// --stills remakes only the console stills (after a console capture) and keeps
// the hero loop, the terminal recordings and the brand files as they are.
async function stillsOnly() {
  const media = JSON.parse(readFileSync(join(SITE, 'data/media.json'), 'utf8'));
  const manifest = JSON.parse(readFileSync(join(SCREENS, 'manifest.json'), 'utf8'));
  media.captured = manifest.captured;
  const names = new Set(STILLS.map((s) => s.name));
  for (const k of Object.keys(media.stills)) if (names.has(k.replace(/-(light|dark)(-m)?$/, ''))) delete media.stills[k];
  for (const f of readdirSync(OUT)) if (names.has(f.replace(/-(light|dark)(-m)?-\d+\.(png|avif|webp)$/, ''))) rmSync(join(OUT, f));
  const toWebp = [];
  for (const s of STILLS) {
    for (const theme of ['light', 'dark']) {
      const src = join(SCREENS, `${s.name}-${theme}.png`);
      media.stills[`${s.name}-${theme}`] = still(src, `${s.name}-${theme}`, s.crop, s.widths, toWebp);
      if (s.phone) media.stills[`${s.name}-${theme}-m`] = still(src, `${s.name}-${theme}-m`, s.phone, [720, 920], toWebp);
    }
  }
  await webp(toWebp);
  writeFileSync(join(SITE, 'data/media.json'), JSON.stringify(media, null, 1) + '\n');
  console.log(`Wrote ${toWebp.length} stills (x3 formats) to public/media`);
}

// --hero-only remakes only the hero loop (after a new hero render) and keeps the
// console stills, the terminal recordings and the brand files as they are.
async function heroOnly() {
  if (!existsSync(join(HERO, 'master.mkv'))) throw new Error(`no hero render at ${join(HERO, 'master.mkv')}; see the note at HERO`);
  const media = JSON.parse(readFileSync(join(SITE, 'data/media.json'), 'utf8'));
  for (const f of readdirSync(OUT)) if (/^hero[.-]/.test(f)) rmSync(join(OUT, f));
  const toWebp = [];
  await heroLoop(media, toWebp);
  await webp(toWebp);
  writeFileSync(join(SITE, 'data/media.json'), JSON.stringify(media, null, 1) + '\n');
  console.log(`Wrote the hero loop and ${toWebp.length} hero stills (x3 formats) to public/media`);
}

async function main() {
  if (process.argv.includes('--demo')) return demo();
  if (process.argv.includes('--stills')) return stillsOnly();
  if (process.argv.includes('--hero-only')) return heroOnly();
  if (!existsSync(SCREENS)) throw new Error(`no captures at ${SCREENS}; pass --brand`);
  rmSync(OUT, { recursive: true, force: true });
  mkdirSync(OUT, { recursive: true });
  const manifest = JSON.parse(readFileSync(join(SCREENS, 'manifest.json'), 'utf8'));
  const media = { captured: manifest.captured, stills: {}, posters: {}, videos: {} };
  const toWebp = [];
  // The captures keep the console's own colours: its pink accent is the site's accent.
  for (const s of STILLS) {
    for (const theme of ['light', 'dark']) {
      const src = join(SCREENS, `${s.name}-${theme}.png`);
      media.stills[`${s.name}-${theme}`] = still(src, `${s.name}-${theme}`, s.crop, s.widths, toWebp);
      if (s.phone) media.stills[`${s.name}-${theme}-m`] = still(src, `${s.name}-${theme}-m`, s.phone, [720, 920], toWebp);
    }
  }
  await heroLoop(media, toWebp);
  for (const p of POSTERS) {
    media.posters[p.name] = still(join(SCREENS, p.src), p.name, p.crop || null, p.widths, toWebp);
    if (p.phone) media.stills[`${p.name}-m`] = still(join(SCREENS, p.src), `${p.name}-m`, p.phone, [720], toWebp);
  }
  for (const { file: v, crop } of VIDEOS) {
    if (!crop) copyFileSync(join(SCREENS, v), join(OUT, v));
    else {
      const vf = `crop=${crop.w}:${crop.h}:${crop.x}:${crop.y}`;
      const codec = v.endsWith('.mp4')
        ? ['-c:v', 'libx264', '-preset', 'slow', '-crf', '22', '-pix_fmt', 'yuv420p', '-movflags', '+faststart']
        : ['-c:v', 'libvpx-vp9', '-b:v', '0', '-crf', '34', '-row-mt', '1', '-pix_fmt', 'yuv420p'];
      ff('-i', join(SCREENS, v), '-an', '-vf', vf, ...codec, join(OUT, v));
    }
    const { w, h, duration } = probe(join(OUT, v));
    media.videos[v] = { w, h, duration: Math.round(duration * 10) / 10, bytes: statSync(join(OUT, v)).size };
  }
  await webp(toWebp);
  writeFileSync(join(SITE, 'data/media.json'), JSON.stringify(media, null, 1) + '\n');

  // Brand files: logo, favicons, Open Graph cards.
  const brandOut = join(SITE, 'public/brand');
  rmSync(brandOut, { recursive: true, force: true });
  mkdirSync(brandOut, { recursive: true });
  for (const f of ['lockup.svg', 'lockup-on-dark.svg', 'mark.svg', 'mark-on-dark.svg']) copyFileSync(join(BRAND, 'logo', f), join(brandOut, f));
  for (const f of ['favicon.ico', 'favicon.svg', 'apple-touch-icon.png', 'icon-192.png', 'icon-512.png', 'icon-maskable-512.png', 'site.webmanifest']) copyFileSync(join(BRAND, 'favicon', f), join(SITE, 'public', f));
  const og = join(BRAND, 'og/out');
  const ogOut = join(SITE, 'public/og');
  rmSync(ogOut, { recursive: true, force: true });
  const copyTree = (from, to) => {
    mkdirSync(to, { recursive: true });
    for (const n of readdirSync(from)) {
      const p = join(from, n);
      if (statSync(p).isDirectory()) copyTree(p, join(to, n));
      else if (n.endsWith('.png') || n === 'index.json') copyFileSync(p, join(to, n));
    }
  };
  if (existsSync(og)) copyTree(og, ogOut);
  console.log(`Wrote ${toWebp.length} stills (x3 formats) and ${VIDEOS.length} videos to public/media`);
}

// heroLoop encodes the hero recording at 1280x720 and 30 fps (MP4 and WebM),
// takes its poster from the middle of the loop, and reads its chapters, which the
// page shows as the list of steps beside the video.
async function heroLoop(media, toWebp) {
  const master = join(HERO, 'master.mkv');
  if (!existsSync(master)) throw new Error(`no hero render at ${master}; see the note at HERO`);
  const vtt = readFileSync(join(HERO, 'chapters.vtt'), 'utf8');
  const secs = (t) => t.split(':').reduce((n, x) => n * 60 + Number(x), 0);
  const all = [...vtt.matchAll(/^(\S+)\n([\d:.]+) --> ([\d:.]+)\n(.+)$/gm)].map((m) => ({ id: m[1], start: secs(m[2]), end: secs(m[3]), text: m[4] }));
  const first = all.find((c) => c.id === HERO_FROM);
  if (!first) throw new Error(`the hero render has no chapter ${HERO_FROM}`);
  const from = first.start;
  const vf = 'scale=1280:720:flags=lanczos,fps=30';
  ff('-ss', String(from), '-i', master, '-an', '-vf', vf, '-c:v', 'libx264', '-profile:v', 'high', '-preset', 'slow', '-crf', '23', '-pix_fmt', 'yuv420p', '-movflags', '+faststart', join(OUT, 'hero.mp4'));
  ff('-ss', String(from), '-i', master, '-an', '-vf', vf, '-c:v', 'libvpx-vp9', '-b:v', '0', '-crf', '35', '-row-mt', '1', '-deadline', 'good', '-cpu-used', '2', '-pix_fmt', 'yuv420p', join(OUT, 'hero.webm'));
  for (const v of ['hero.mp4', 'hero.webm']) {
    const { w, h, duration } = probe(join(OUT, v));
    media.videos[v] = { w, h, duration: Math.round(duration * 10) / 10, bytes: statSync(join(OUT, v)).size };
  }
  const png = join(tmpdir(), `5050site-hero-poster-${process.pid}.png`);
  ff('-ss', String(HERO_POSTER_AT), '-i', join(OUT, 'hero.mp4'), '-frames:v', '1', png);
  media.posters['hero-poster'] = still(png, 'hero-poster', null, [800, 1280], toWebp);
  if (!existsSync(HERO_PHONE.take)) throw new Error(`no hero take frame at ${HERO_PHONE.take}`);
  media.posters['hero-phone'] = still(HERO_PHONE.take, 'hero-phone', HERO_PHONE.crop, HERO_PHONE.widths, toWebp);
  const runFile = join(HERO, '..', 'run.json');
  const date = existsSync(runFile) ? JSON.parse(readFileSync(runFile, 'utf8')).date : null;
  const r2 = (n) => Math.round(n * 100) / 100;
  const chapters = all.filter((c) => c.start >= from).map((c) => ({ id: c.id, start: r2(c.start - from), end: r2(c.end - from), text: c.text, caption: HERO_CAPTIONS[c.id] }));
  for (const c of chapters) if (!c.caption) throw new Error(`no caption written for hero step ${c.id}`);
  media.hero = { date, chapters };
  if (chapters.length < 4) throw new Error('the hero render has too few chapters');
  // Captions and chapters as WebVTT, next to the video.
  const ts = (n) => { const ms = Math.round(n * 1000); const h = Math.floor(ms / 3600000), m = Math.floor(ms / 60000) % 60, s = Math.floor(ms / 1000) % 60; return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}.${String(ms % 1000).padStart(3, '0')}`; };
  writeFileSync(join(OUT, 'hero.en.vtt'), 'WEBVTT\n\n' + chapters.map((c, i) => `${i + 1}\n${ts(c.start)} --> ${ts(c.end)}\n${c.caption}\n`).join('\n'));
  writeFileSync(join(OUT, 'hero.chapters.vtt'), 'WEBVTT\n\n' + chapters.map((c) => `${c.id}\n${ts(c.start)} --> ${ts(c.end)}\n${c.text}\n`).join('\n'));
}

// What each step of the hero recording shows, in one plain sentence. These are
// the captions track and the transcript under the video.
const HERO_CAPTIONS = {
  image: 'On an empty canvas, Add, then Docker Image: nginx:1.27 is staged and deployed. The wait is sped up and labelled.',
  postgres: 'Add Postgres. It runs as its own service with a volume.',
  variables: 'Set DATABASE_URL on nginx to ${{Postgres.DATABASE_URL}}, then redeploy.',
  domain: 'In Settings, Networking, Generate Domain gives nginx a public HTTPS name.',
  open: 'Open the name: the nginx welcome page, served over HTTPS.',
};

// demo copies a finished render of the 90 s demo into public/demo.
async function demo() {
  const dir = resolve(arg('--demo'));
  if (!process.argv.includes('--reviewed')) {
    throw new Error('Watch the whole render first: it must show no private hostname, IP, sslip.io name, token or account name. Then run again with --reviewed.');
  }
  const need = ['5050cloud-demo.mp4', '5050cloud-demo.webm', 'poster.png'];
  for (const f of need) if (!existsSync(join(dir, f))) throw new Error(`${dir} has no ${f}: the render is not finished`);
  const out = join(SITE, 'public/demo');
  rmSync(out, { recursive: true, force: true });
  mkdirSync(out, { recursive: true });
  for (const v of ['5050cloud-demo.mp4', '5050cloud-demo.webm']) {
    const { duration } = probe(join(dir, v));
    if (!(duration > 30)) throw new Error(`${v} is ${duration} s long: the render is not finished`);
    if (statSync(join(dir, v)).size > 16 * 1024 * 1024) throw new Error(`${v} is over 16 MB; encode a 1280x720 copy first`);
    copyFileSync(join(dir, v), join(out, v));
  }
  const vtt = ['5050cloud-demo.en.vtt', 'demo.en.vtt'].map((f) => join(dir, f)).find(existsSync);
  if (!vtt) throw new Error('the render has no captions file');
  copyFileSync(vtt, join(out, '5050cloud-demo.en.vtt'));
  if (existsSync(join(dir, 'chapters.vtt'))) copyFileSync(join(dir, 'chapters.vtt'), join(out, 'chapters.vtt'));
  mkdirSync(OUT, { recursive: true });
  const toWebp = [];
  const saved = OUT;
  const p = still(join(dir, 'poster.png'), 'demo-poster', null, [960, 1920], toWebp);
  for (const f of readdirSync(saved).filter((n) => n.startsWith('demo-poster-'))) copyFileSync(join(saved, f), join(out, f.replace('demo-poster-', 'poster-')));
  for (const f of readdirSync(saved).filter((n) => n.startsWith('demo-poster-'))) rmSync(join(saved, f));
  await webp(toWebp.map(([png, w]) => [join(out, png.split('/').pop().replace('demo-poster-', 'poster-')), join(out, w.split('/').pop().replace('demo-poster-', 'poster-'))]));
  writeFileSync(join(out, 'demo.json'), JSON.stringify({ poster: p, duration: probe(join(out, '5050cloud-demo.mp4')).duration }, null, 1) + '\n');
  console.log('Copied the demo into public/demo');
}

main().catch((err) => {
  console.error(err.message);
  process.exit(1);
});
