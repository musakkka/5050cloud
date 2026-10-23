// SPDX-License-Identifier: MIT
// Makes the site's pictures and recordings from the brand repo's real console
// captures (5050cloud-brand/screens). Run it after the captures change; its
// output in public/media and data/media.json is committed, so `npm run build`
// stays dependency-free.
//
//   node scripts/media.mjs [--brand ../5050cloud-brand]
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
// URL, generate a domain and open it, recorded on a fresh VM, with the violet
// accent recoloured and the sslip.io names replaced by example.com names.
const HERO = resolve(arg('--hero', join(SITE, '..', '5050cloud-demo', 'work', 'hero')));
// The loop starts at the first console step: the install command is printed
// as text right beside it, so the recording does not open on a terminal. The
// poster is the moment DATABASE_URL is set to ${{Postgres.DATABASE_URL}}, with
// the reference suggested as it is typed: a readable piece of the console, and
// the Railway syntax the page is about.
const HERO_FROM = arg('--hero-from', 'image');
const HERO_POSTER_AT = Number(arg('--hero-poster-at', '22'));
// The phone picture: the same dialog from the capture itself (3200 x 1800),
// cropped to the dialog so its text stays readable at 390 px.
const HERO_PHONE = { take: join(HERO, '..', 'takes-hero', 'deploy', 'frames', '000076.jpg'), crop: { x: 1060, y: 548, w: 1080, h: 810 } };
const SCREENS = join(BRAND, 'screens');
const OUT = join(SITE, 'public/media');

// Crops are in the capture's own pixels (2880 x 1800, a 1440 x 900 window at
// 2x). Each one ends at a clean boundary: a whole field, a whole row, the edge
// of the window, never through a line of text. `phone` is a second crop of the
// part that matters, at 4:3, served below 720 px so the text stays readable.
const H = (x, y, w, h) => ({ x: x * 2, y: y * 2, w: w * 2, h: h * 2 });
const STILLS = [
  { name: 'canvas', crop: null, phone: H(598, 250, 690, 518), widths: [800, 1600, 2400] },
  { name: 'palette', crop: null, phone: H(980, 62, 460, 345), widths: [800, 1600, 2400] },
  { name: 'metrics', crop: null, phone: H(436, 266, 480, 360), widths: [800, 1600, 2400] },
  // The worker in the screenshot project prints sample job lines; the landing
  // page does not use this picture, only the log filters reference page does.
  { name: 'logs', crop: H(415, 56, 1025, 641), widths: [800, 1600] },
  { name: 'deployments', crop: H(415, 56, 1025, 641), widths: [800, 1600] },
  // Variables: the panel from its title to the provided variables line.
  { name: 'variables', crop: H(425, 66, 990, 500), phone: H(436, 274, 584, 292), widths: [800, 1600] },
  // Networking: the section heading to the private domain note.
  { name: 'networking', crop: H(425, 212, 815, 424), phone: H(434, 214, 500, 375), widths: [800, 1600] },
  // Scale: the whole window; the phone crop keeps Replicas to Use all cores.
  { name: 'scale', crop: null, phone: H(436, 262, 800, 600), widths: [800, 1600, 2400] },
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

// recolour takes the console's violet accent out of the captures (site rules
// C4: no violet anywhere on the page). Light captures: each pixel is read as a
// blend of the accent, rgb(134, 58, 218), and what is behind it, and the accent
// share becomes ink, so a violet button turns into an ink button with its white
// label intact. Dark captures: violet pixels go to their own lightness in grey,
// so violet text on the dark ground stays readable. Other hues (status green,
// the blue Postgres mark, amber notes) are left alone.
async function recolour(jobs) {
  const pw = await import(pathToFileURL(join(BRAND, 'node_modules/playwright-core/index.mjs')).href);
  const browser = await pw.chromium.launch();
  const page = await browser.newPage();
  for (const [src, out, theme] of jobs) {
    const b64 = readFileSync(src).toString('base64');
    const data = await page.evaluate(async ({ url, theme }) => {
      const img = new Image();
      img.src = url;
      await img.decode();
      const c = document.createElement('canvas');
      c.width = img.naturalWidth;
      c.height = img.naturalHeight;
      const g = c.getContext('2d');
      g.drawImage(img, 0, 0);
      const im = g.getImageData(0, 0, c.width, c.height);
      const d = im.data;
      const P = [134, 58, 218], T = [28, 28, 26];
      for (let i = 0; i < d.length; i += 4) {
        const r = d[i], gg = d[i + 1], b = d[i + 2];
        const mx = Math.max(r, gg, b), mn = Math.min(r, gg, b), ch = mx - mn;
        if (ch < 6 || b < r || b < gg) continue;
        let h = mx === r ? ((gg - b) / ch) % 6 : mx === gg ? (b - r) / ch + 2 : (r - gg) / ch + 4;
        h = (h * 60 + 360) % 360;
        const wh = h >= 250 && h <= 295 ? 1 : h > 235 && h < 250 ? (h - 235) / 15 : h > 295 && h < 310 ? (310 - h) / 15 : 0;
        if (!wh) continue;
        if (theme === 'light') {
          const a = Math.min(1, ch / 160) * wh;
          d[i] = r + a * (T[0] - P[0]);
          d[i + 1] = gg + a * (T[1] - P[1]);
          d[i + 2] = b + a * (T[2] - P[2]);
        } else {
          const a = Math.min(1, ch / 100) * wh;
          const y = Math.min(255, (0.299 * r + 0.587 * gg + 0.114 * b) * 1.2);
          d[i] = r + a * (y - r);
          d[i + 1] = gg + a * (y - gg);
          d[i + 2] = b + a * (y - b);
        }
      }
      g.putImageData(im, 0, 0);
      return c.toDataURL('image/png');
    }, { url: `data:image/png;base64,${b64}`, theme });
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

async function main() {
  if (process.argv.includes('--demo')) return demo();
  if (!existsSync(SCREENS)) throw new Error(`no captures at ${SCREENS}; pass --brand`);
  rmSync(OUT, { recursive: true, force: true });
  mkdirSync(OUT, { recursive: true });
  const manifest = JSON.parse(readFileSync(join(SCREENS, 'manifest.json'), 'utf8'));
  const media = { captured: manifest.captured, stills: {}, posters: {}, videos: {} };
  const toWebp = [];
  const TMP = join(tmpdir(), `5050site-recolour-${process.pid}`);
  mkdirSync(TMP, { recursive: true });
  await recolour(STILLS.flatMap((s) => ['light', 'dark'].map((theme) => [join(SCREENS, `${s.name}-${theme}.png`), join(TMP, `${s.name}-${theme}.png`), theme])));
  for (const s of STILLS) {
    for (const theme of ['light', 'dark']) {
      const src = join(TMP, `${s.name}-${theme}.png`);
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
// takes its poster from the variables step, and reads its chapters, which the
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
  ff('-ss', String(HERO_POSTER_AT), '-i', master, '-frames:v', '1', png);
  media.posters['hero-poster'] = still(png, 'hero-poster', null, [800, 1600], toWebp);
  if (!existsSync(HERO_PHONE.take)) throw new Error(`no hero take frame at ${HERO_PHONE.take}`);
  media.posters['hero-phone'] = still(HERO_PHONE.take, 'hero-phone', HERO_PHONE.crop, [720, 1080], toWebp);
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
  image: 'Create, then Docker Image: nginx:1.27 deploys to an empty canvas. The wait is sped up and labelled.',
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
