// SPDX-License-Identifier: MIT
// A small Markdown renderer for the docs pages. It covers what the pages use:
// front matter, headings, paragraphs, lists (one level, with continuation
// lines), fenced code, tables, blockquote callouts, and inline code, bold,
// italic and links. Anything else is rendered as escaped text.

export function escapeHtml(s) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

export function slugify(s) {
  return s
    .toLowerCase()
    .replace(/<[^>]+>/g, '')
    .replace(/[`*_]/g, '')
    .replace(/[^a-z0-9 -]/g, '')
    .trim()
    .replace(/\s+/g, '-')
    .replace(/-+/g, '-');
}

export function parseFrontMatter(src) {
  const data = {};
  let body = src;
  if (src.startsWith('---\n')) {
    const end = src.indexOf('\n---\n', 4);
    if (end < 0) throw new Error('unterminated front matter');
    for (const line of src.slice(4, end).split('\n')) {
      const m = line.match(/^([A-Za-z_]+):\s*(.*)$/);
      if (m) data[m[1]] = m[2].replace(/^"(.*)"$/, '$1');
    }
    body = src.slice(end + 5);
  }
  return { data, body };
}

// inline renders inline Markdown. linkFn rewrites hrefs (relative links).
export function inline(text, linkFn = (h) => h) {
  const out = [];
  let i = 0;
  // Split off code spans first so their contents are never formatted.
  const parts = text.split(/(`+[^`]*?`+)/g);
  for (const part of parts) {
    if (/^`+[^`]*`+$/.test(part) && part.length >= 2) {
      const code = part.replace(/^`+/, '').replace(/`+$/, '');
      out.push(`<code>${escapeHtml(code)}</code>`);
      continue;
    }
    let s = escapeHtml(part);
    s = s.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_, t, h) => {
      const href = linkFn(h.replace(/&amp;/g, '&'));
      const ext = /^https?:/.test(href) ? ' rel="noopener"' : '';
      return `<a href="${escapeHtml(href)}"${ext}>${t}</a>`;
    });
    s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    s = s.replace(/(^|[\s(])_([^_]+)_(?=[\s.,;:)!?]|$)/g, '$1<em>$2</em>');
    // Release promises do not go inline: pages link to the one "Not yet" list.
    if (/\[\[0\.1\.1\]\]/.test(s)) throw new Error('[[0.1.1]] notes are retired: link to /docs/reference/honest-limits#not-yet');
    out.push(s);
    i++;
  }
  return out.join('');
}

function splitRow(line) {
  let l = line.trim();
  if (l.startsWith('|')) l = l.slice(1);
  if (l.endsWith('|')) l = l.slice(0, -1);
  // Split on pipes that are not escaped and not inside code spans.
  const cells = [];
  let cur = '';
  let inCode = false;
  for (let k = 0; k < l.length; k++) {
    const c = l[k];
    if (c === '\\' && l[k + 1] === '|') { cur += '|'; k++; continue; }
    if (c === '`') inCode = !inCode;
    if (c === '|' && !inCode) { cells.push(cur.trim()); cur = ''; continue; }
    cur += c;
  }
  cells.push(cur.trim());
  return cells;
}

// render turns a Markdown body into HTML and collects h2/h3 headings.
export function render(body, linkFn) {
  const lines = body.replace(/\r\n/g, '\n').split('\n');
  const html = [];
  const toc = [];
  const used = new Set();
  let i = 0;
  const isBlockStart = (l) =>
    /^(#{1,6} |```|> |\s*[-*] |\s*\d+\. |\|)/.test(l) || l.trim() === '';

  while (i < lines.length) {
    const line = lines[i];
    if (line.trim() === '') { i++; continue; }

    if (line.startsWith('```')) {
      const lang = line.slice(3).trim();
      const buf = [];
      i++;
      while (i < lines.length && !lines[i].startsWith('```')) buf.push(lines[i++]);
      i++;
      const cls = lang ? ` class="lang-${escapeHtml(lang)}"` : '';
      // Output blocks (```text) are shown as output with no copy button.
      if (lang === 'text' || lang === 'output') {
        html.push(`<div class="code out"><p class="code-label">Output</p><pre${cls}><code>${escapeHtml(buf.join('\n'))}</code></pre></div>`);
      } else {
        const label = /^(sh|bash|shell|console)$/.test(lang) ? 'Copy command' : 'Copy code';
        html.push(`<div class="code"><pre${cls}><code>${escapeHtml(buf.join('\n'))}</code></pre><button class="copy" type="button" aria-label="${label}">Copy</button></div>`);
      }
      continue;
    }

    const h = line.match(/^(#{1,6}) (.*)$/);
    if (h) {
      const level = h[1].length;
      let id = slugify(h[2]);
      let n = 2;
      while (used.has(id)) id = `${slugify(h[2])}-${n++}`;
      used.add(id);
      const text = inline(h[2], linkFn);
      if (level === 2 || level === 3) toc.push({ level, id, text: h[2].replace(/[`*]/g, '') });
      html.push(`<h${level} id="${id}">${text}</h${level}>`);
      i++;
      continue;
    }

    if (line.startsWith('|')) {
      const rows = [];
      while (i < lines.length && lines[i].startsWith('|')) rows.push(lines[i++]);
      const head = splitRow(rows[0]);
      const bodyRows = rows.slice(rows[1] && /^\|?\s*:?-+/.test(rows[1]) ? 2 : 1);
      const th = head.map((c) => `<th scope="col">${inline(c, linkFn)}</th>`).join('');
      // Each cell carries its column name, so the table can stack into
      // labelled rows on a phone; short code (flags, names) never breaks.
      const label = head.map((c) => escapeHtml(c.replace(/[`*_]/g, '')));
      const cell = (c) => inline(c, linkFn).replace(/<code>([^<]{1,28})<\/code>/g, '<code class="nw">$1</code>');
      const trs = bodyRows
        .map((r) => `<tr>${splitRow(r).map((c, k) => `<td data-label="${label[k] || ''}">${cell(c)}</td>`).join('')}</tr>`)
        .join('');
      html.push(`<div class="table stack-table"><table><thead><tr>${th}</tr></thead><tbody>${trs}</tbody></table></div>`);
      continue;
    }

    if (line.startsWith('> ')) {
      const buf = [];
      while (i < lines.length && lines[i].startsWith('>')) buf.push(lines[i++].replace(/^> ?/, ''));
      let kind = 'note';
      const first = buf[0].match(/^\*\*(Note|Tip|Warning|Caution|Railway difference|Not yet|Beta):?\*\*:?\s*/i);
      if (first) kind = first[1].toLowerCase().replace(/[^a-z0-9]+/g, '-');
      html.push(`<aside class="callout ${kind}">${render(buf.join('\n'), linkFn).html}</aside>`);
      continue;
    }

    const li = line.match(/^(\s*)([-*]|\d+\.) (.*)$/);
    if (li) {
      const ordered = /\d/.test(li[2]);
      const items = [];
      while (i < lines.length) {
        const m = lines[i].match(/^(\s*)([-*]|\d+\.) (.*)$/);
        if (m && (/\d/.test(m[2]) === ordered || m[1].length > 0)) {
          if (m[1].length > 0 && items.length) {
            items[items.length - 1].sub.push({ ordered: /\d/.test(m[2]), text: m[3] });
          } else {
            items.push({ text: m[3], sub: [] });
          }
          i++;
          continue;
        }
        if (lines[i].trim() !== '' && /^\s+\S/.test(lines[i]) && items.length) {
          const last = items[items.length - 1];
          if (last.sub.length) last.sub[last.sub.length - 1].text += ' ' + lines[i].trim();
          else last.text += ' ' + lines[i].trim();
          i++;
          continue;
        }
        break;
      }
      const tag = ordered ? 'ol' : 'ul';
      const startNum = ordered ? parseInt(li[2], 10) : 1;
      const startAttr = ordered && startNum > 1 ? ` start="${startNum}"` : '';
      const inner = items
        .map((it) => {
          let sub = '';
          if (it.sub.length) {
            const st = it.sub[0].ordered ? 'ol' : 'ul';
            sub = `<${st}>${it.sub.map((s) => `<li>${inline(s.text, linkFn)}</li>`).join('')}</${st}>`;
          }
          return `<li>${inline(it.text, linkFn)}${sub}</li>`;
        })
        .join('');
      html.push(`<${tag}${startAttr}>${inner}</${tag}>`);
      continue;
    }

    const buf = [line];
    i++;
    while (i < lines.length && !isBlockStart(lines[i])) buf.push(lines[i++]);
    html.push(`<p>${inline(buf.join(' '), linkFn)}</p>`);
  }
  return { html: html.join('\n'), toc };
}
