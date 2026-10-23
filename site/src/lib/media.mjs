// SPDX-License-Identifier: MIT
// Pictures and recordings of the real console, made by scripts/media.mjs and
// listed in data/media.json. Used by the landing page and the docs.

import { escapeHtml as e } from './markdown.mjs';

// picture renders one still in AVIF, WebP and PNG at every width it was made in.
export function picture(r, media, key, { alt, sizes, eager = false, cls = '', phone = true }) {
  const m = media.stills[key] || media.posters[key];
  if (!m) throw new Error(`no media for ${key}; run node scripts/media.mjs`);
  const [W, H] = m.ratio;
  const set = (ext) => m.widths.map((w) => `${r(`/media/${key}-${w}.${ext}`)} ${w}w`).join(', ');
  const mid = m.widths[Math.min(1, m.widths.length - 1)];
  // A phone crop (made by scripts/media.mjs) replaces the whole screen below
  // 720 px, so the part that matters stays readable (site rules L9).
  const ph = phone ? media.stills[`${key}-m`] : null;
  const phSet = (ext) => ph.widths.map((w) => `${r(`/media/${key}-m-${w}.${ext}`)} ${w}w`).join(', ');
  const phoneSrc = ph ? `<source media="(max-width: 720px)" type="image/avif" srcset="${phSet('avif')}" sizes="100vw" width="${ph.ratio[0]}" height="${ph.ratio[1]}">
<source media="(max-width: 720px)" type="image/webp" srcset="${phSet('webp')}" sizes="100vw" width="${ph.ratio[0]}" height="${ph.ratio[1]}">
` : '';
  return `<picture class="${cls}">
${phoneSrc}<source type="image/avif" srcset="${set('avif')}" sizes="${sizes}">
<source type="image/webp" srcset="${set('webp')}" sizes="${sizes}">
<img src="${r(`/media/${key}-${mid}.png`)}" alt="${e(alt)}" width="${W}" height="${H}"${eager ? ' fetchpriority="high"' : ' loading="lazy" decoding="async"'}>
</picture>`;
}

// shot renders a light and a dark capture; CSS shows the one for the theme.
export function shot(r, media, name, opts) {
  return `${picture(r, media, `${name}-light`, { ...opts, cls: 'only-light' })}${picture(r, media, `${name}-dark`, { ...opts, cls: 'only-dark' })}`;
}

// loop is a short, real recording that plays muted in a loop while it is in
// view. site.js starts it only without reduced motion; otherwise the poster
// stays, and the button plays it on request.
export function loop(r, media, { name, poster, label, cls = '', eager = false }) {
  const v = media.videos[`${name}.mp4`];
  const p = media.posters[poster];
  const posterSrc = r(`/media/${poster}-${p.widths[p.widths.length - 1]}.webp`);
  return `<div class="loop ${cls}">
<video class="loop-video" muted loop playsinline preload="none" poster="${posterSrc}" width="${v.w}" height="${v.h}" aria-label="${e(label)}" data-autoplay${eager ? ' data-eager' : ''}>
<source src="${r(`/media/${name}.mp4`)}" type="video/mp4">
<source src="${r(`/media/${name}.webm`)}" type="video/webm">
</video>
<button class="loop-toggle" type="button" hidden>Play</button>
</div>`;
}

