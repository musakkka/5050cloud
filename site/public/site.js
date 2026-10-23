// SPDX-License-Identifier: MIT
// The site's only script. Every part works on its own and the page is
// complete without it: copy buttons, the phone menu, the theme toggle, docs
// search, the cost calculator, section entrances and demo playback.
(function () {
  var doc = document;
  var root = doc.documentElement;
  var base = doc.body.getAttribute('data-root') || '/';
  var announce = doc.getElementById('announce');
  var say = function (msg) { if (announce) { announce.textContent = ''; setTimeout(function () { announce.textContent = msg; }, 30); } };
  var store = {
    get: function (k) { try { return localStorage.getItem(k); } catch (e) { return null; } },
    set: function (k, v) { try { if (v === null) localStorage.removeItem(k); else localStorage.setItem(k, v); } catch (e) {} },
  };

  // Copy buttons (K3, K4). Without the Clipboard API the text is selected.
  doc.querySelectorAll('.code .copy').forEach(function (b) {
    var label = b.textContent;
    var name = b.getAttribute('aria-label') || label;
    var show = function (text) {
      b.textContent = text;
      b.setAttribute('aria-label', text === label ? name : text);
      setTimeout(function () { b.textContent = label; b.setAttribute('aria-label', name); }, 2000);
    };
    b.addEventListener('click', function () {
      var codeEl = b.parentNode.querySelector('code');
      var text = codeEl.textContent;
      var done = function () {
        show('Copied');
        say('Copied to the clipboard');
      };
      var select = function () {
        var range = doc.createRange();
        range.selectNodeContents(codeEl);
        var sel = window.getSelection();
        sel.removeAllRanges();
        sel.addRange(range);
        show('Selected');
        say('Command selected. Press Control C or Command C to copy it.');
      };
      if (navigator.clipboard && window.isSecureContext) {
        var settled = false;
        var finish = function (fn) { return function () { if (!settled) { settled = true; fn(); } }; };
        navigator.clipboard.writeText(text).then(finish(done), finish(select));
        setTimeout(finish(select), 1500);
      } else select();
    });
  });

  // A code block wider than its box says so, instead of looking clipped.
  var markWide = function () {
    doc.querySelectorAll('.code pre').forEach(function (pre) {
      var box = pre.parentNode;
      var wide = pre.scrollWidth > pre.clientWidth + 2;
      var note = box.querySelector('.scroll-note');
      if (wide && !note) {
        note = doc.createElement('p');
        note.className = 'scroll-note';
        note.textContent = 'Scroll sideways for the rest of the line.';
        box.appendChild(note);
        pre.setAttribute('tabindex', '0');
      } else if (!wide && note) {
        box.removeChild(note);
        pre.removeAttribute('tabindex');
      }
    });
  };
  markWide();
  var wideTimer = null;
  window.addEventListener('resize', function () { clearTimeout(wideTimer); wideTimer = setTimeout(markWide, 150); });

  // Phone menu (K1): a full-screen sheet, Escape closes it, focus returns.
  var sheet = doc.getElementById('sheet');
  var opener = doc.querySelector('.menu-open');
  var closer = doc.querySelector('.menu-close');
  function openSheet() {
    sheet.hidden = false;
    sheet.classList.add('opening');
    opener.setAttribute('aria-expanded', 'true');
    doc.body.style.overflow = 'hidden';
    var cur = sheet.querySelector('[aria-current="page"]');
    (cur || closer).focus();
  }
  function closeSheet() {
    if (sheet.hidden) return;
    sheet.hidden = true;
    sheet.classList.remove('opening');
    opener.setAttribute('aria-expanded', 'false');
    doc.body.style.overflow = '';
    opener.focus();
  }
  if (sheet && opener && closer) {
    opener.addEventListener('click', openSheet);
    closer.addEventListener('click', closeSheet);
    sheet.addEventListener('click', function (ev) { if (ev.target.closest('a')) closeSheet(); });
    doc.addEventListener('keydown', function (ev) { if (ev.key === 'Escape') closeSheet(); });
  }

  // Theme toggle (K9): system, light, dark, kept per viewer.
  var themes = ['system', 'light', 'dark'];
  // The boot script in <head> already applied a stored choice or a ?theme= override.
  var current = root.getAttribute('data-theme') || 'system';
  if (themes.indexOf(current) < 0) current = 'system';
  var toggles = doc.querySelectorAll('.theme-toggle');
  function applyTheme(t, keep) {
    current = t;
    if (t === 'system') root.removeAttribute('data-theme');
    else root.setAttribute('data-theme', t);
    if (!keep) store.set('5050site.theme', t === 'system' ? null : t);
    var word = { system: 'Auto theme', light: 'Light theme', dark: 'Dark theme' }[t];
    toggles.forEach(function (b) { b.textContent = word; b.setAttribute('aria-label', 'Theme: ' + t + '. Select to change.'); });
  }
  toggles.forEach(function (b) {
    b.hidden = false;
    b.addEventListener('click', function () {
      var next = themes[(themes.indexOf(current) + 1) % themes.length];
      applyTheme(next);
      say('Theme set to ' + next);
    });
  });
  applyTheme(current, true);

  // Docs search: a dialog over search.json, loaded on first use.
  var dialog = doc.getElementById('search');
  var openers = doc.querySelectorAll('.search-open');
  if (dialog && typeof dialog.showModal === 'function') {
    var input = dialog.querySelector('input');
    var list = dialog.querySelector('.search-results');
    var status = dialog.querySelector('.search-status');
    var index = null;
    var lastFocus = null;
    var load = function () {
      if (index) return Promise.resolve(index);
      return fetch(base + 'search.json').then(function (r) { return r.json(); }).then(function (j) { index = j; return j; });
    };
    var esc = function (s) { return s.replace(/[&<>"]/g, function (c) { return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]; }); };
    var run = function () {
      var q = input.value.trim().toLowerCase();
      if (!q) { list.innerHTML = ''; status.textContent = 'Type to search ' + (index ? index.length : '') + ' pages.'; return; }
      var words = q.split(/\s+/).filter(Boolean);
      var scored = [];
      index.forEach(function (p) {
        var t = p.t.toLowerCase(), d = p.d.toLowerCase(), h = p.h.join(' ').toLowerCase(), x = p.x.toLowerCase();
        var score = 0;
        for (var i = 0; i < words.length; i++) {
          var w = words[i], s = 0;
          if (t.indexOf(w) >= 0) s += 10;
          if (h.indexOf(w) >= 0) s += 5;
          if (d.indexOf(w) >= 0) s += 3;
          if (x.indexOf(w) >= 0) s += 1;
          if (!s) { score = 0; break; }
          score += s;
        }
        if (t === q) score += 20;
        if (score) scored.push({ p: p, s: score });
      });
      scored.sort(function (a, b) { return b.s - a.s; });
      var top = scored.slice(0, 12);
      list.innerHTML = top.map(function (r) {
        return '<li><a href="' + base + esc(r.p.u) + '"><strong>' + esc(r.p.t) + '</strong><span>' + esc(r.p.d) + '</span></a></li>';
      }).join('');
      status.textContent = scored.length ? scored.length + (scored.length === 1 ? ' page matches.' : ' pages match.') : 'No page matches. Try fewer words.';
    };
    var show = function () {
      lastFocus = doc.activeElement;
      closeSheet();
      dialog.showModal();
      input.focus();
      status.textContent = 'Loading the index';
      load().then(run, function () { status.textContent = 'The search index did not load. The docs home lists every page.'; });
    };
    openers.forEach(function (b) { b.hidden = false; b.addEventListener('click', show); });
    input.addEventListener('input', function () { if (index) run(); });
    input.addEventListener('keydown', function (ev) {
      if (ev.key === 'Enter') { var first = list.querySelector('a'); if (first) { ev.preventDefault(); location.href = first.href; } }
      if (ev.key === 'ArrowDown') { var a = list.querySelector('a'); if (a) { ev.preventDefault(); a.focus(); } }
    });
    list.addEventListener('keydown', function (ev) {
      var links = Array.prototype.slice.call(list.querySelectorAll('a'));
      var i = links.indexOf(doc.activeElement);
      if (ev.key === 'ArrowDown' && i < links.length - 1) { ev.preventDefault(); links[i + 1].focus(); }
      if (ev.key === 'ArrowUp') { ev.preventDefault(); (i > 0 ? links[i - 1] : input).focus(); }
    });
    dialog.addEventListener('close', function () { if (lastFocus && lastFocus.focus) lastFocus.focus(); });
    dialog.addEventListener('click', function (ev) { if (ev.target === dialog) dialog.close(); });
    doc.addEventListener('keydown', function (ev) {
      var tag = (ev.target.tagName || '').toLowerCase();
      var typing = tag === 'input' || tag === 'textarea' || ev.target.isContentEditable;
      if ((ev.key === 'k' && (ev.metaKey || ev.ctrlKey)) || (ev.key === '/' && !typing)) {
        if (!dialog.open) { ev.preventDefault(); show(); }
      }
    });
  }

  var reduce = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  var canWatch = 'IntersectionObserver' in window;

  // Tabs: the console tour (links to each panel) and the workload switch in
  // the price section. Without script every panel is shown, one under another.
  function tabset(list) {
    var tabs = Array.prototype.slice.call(list.querySelectorAll('[role="tab"]'));
    var select = function (tab, focus) {
      tabs.forEach(function (t) {
        var on = t === tab;
        t.setAttribute('aria-selected', String(on));
        t.setAttribute('tabindex', on ? '0' : '-1');
        var panel = doc.getElementById(t.getAttribute('aria-controls'));
        if (panel) { if (on) panel.removeAttribute('data-hidden'); else panel.setAttribute('data-hidden', ''); }
      });
      if (focus) tab.focus();
    };
    tabs.forEach(function (t, i) {
      t.setAttribute('tabindex', t.getAttribute('aria-selected') === 'true' ? '0' : '-1');
      t.addEventListener('click', function (ev) { ev.preventDefault(); select(t); });
      t.addEventListener('keydown', function (ev) {
        var k = ev.key, j = -1;
        if (k === 'ArrowRight') j = (i + 1) % tabs.length;
        else if (k === 'ArrowLeft') j = (i - 1 + tabs.length) % tabs.length;
        else if (k === 'Home') j = 0;
        else if (k === 'End') j = tabs.length - 1;
        else if (k === ' ' && t.tagName === 'A') { ev.preventDefault(); select(t); }
        if (j >= 0) { ev.preventDefault(); select(tabs[j], true); }
      });
    });
    list.hidden = false;
  }
  doc.querySelectorAll('[role="tablist"]').forEach(tabset);
  root.classList.add('ready');

  // Loops: short real recordings. They play muted while in view, never under
  // reduced motion unless asked, and always have a visible Play or Pause button.
  doc.querySelectorAll('.loop').forEach(function (box) {
    var v = box.querySelector('video');
    var b = box.querySelector('.loop-toggle');
    if (!v || !b) return;
    var held = reduce;
    var label = function () {
      var word = v.paused ? 'Play' : 'Pause';
      b.textContent = word;
      b.setAttribute('aria-label', word + ' the recording');
    };
    var play = function () { v.muted = true; var p = v.play(); if (p && p.catch) p.catch(function () { label(); }); };
    b.hidden = false;
    b.addEventListener('click', function () {
      if (v.paused) { held = false; play(); } else { held = true; v.pause(); }
    });
    v.addEventListener('play', label);
    v.addEventListener('pause', label);
    label();
    if (!canWatch) { if (!held) play(); return; }
    new IntersectionObserver(function (entries) {
      entries.forEach(function (en) {
        if (en.isIntersecting && !held) play();
        else if (!en.isIntersecting && !v.paused) v.pause();
      });
    }, { threshold: 0.35 }).observe(v);
  });

  // The hero recording (M3, 8.2): plays muted in a loop while in view, never
  // on its own under reduced motion, with a visible Play or Pause button. The
  // steps under it mark the one playing, and each jumps the recording there.
  var hero = doc.querySelector('.hero-video');
  var heroBtn = doc.querySelector('.hero-toggle');
  var heroScreen = doc.querySelector('.hero-screen');
  // On a phone the screen shows a readable still; the button swaps in the
  // recording at full width with the browser's own controls.
  var phone = window.matchMedia && window.matchMedia('(max-width: 720px)');
  var isPhone = function () { return !!(phone && phone.matches); };
  if (hero && heroBtn) {
    var stepEls = Array.prototype.slice.call(doc.querySelectorAll('.step'));
    var heroHeld = reduce;
    hero.loop = true;
    var heroLabel = function () {
      var word = hero.paused ? 'Play' : 'Pause';
      heroBtn.textContent = isPhone() && hero.paused ? 'Play the ' + Math.round(hero.duration || 27) + ' s recording' : word;
      heroBtn.setAttribute('aria-label', word + ' the recording');
    };
    var heroPlay = function () {
      if (isPhone()) { heroScreen.classList.add('is-playing'); hero.controls = true; }
      hero.muted = true; var p = hero.play(); if (p && p.catch) p.catch(heroLabel);
    };
    var mark = function () {
      var t = hero.currentTime;
      stepEls.forEach(function (b) {
        var a = Number(b.getAttribute('data-at')), z = Number(b.getAttribute('data-end'));
        var now = t >= a && t < z;
        b.classList.toggle('is-now', now);
        b.classList.toggle('is-done', t >= z);
        b.style.setProperty('--p', now ? ((t - a) / (z - a)).toFixed(3) : '0');
        if (now) b.setAttribute('aria-current', 'step'); else b.removeAttribute('aria-current');
      });
    };
    var tick = function () { mark(); if (!hero.paused) requestAnimationFrame(tick); };
    heroBtn.hidden = false;
    heroBtn.addEventListener('click', function () {
      if (hero.paused) { heroHeld = false; heroPlay(); } else { heroHeld = true; hero.pause(); }
    });
    stepEls.forEach(function (b) {
      b.addEventListener('click', function () {
        hero.currentTime = Number(b.getAttribute('data-at')) + 0.05;
        heroHeld = false;
        heroPlay();
        mark();
      });
    });
    hero.addEventListener('play', function () { heroLabel(); requestAnimationFrame(tick); });
    hero.addEventListener('pause', function () { heroLabel(); mark(); });
    hero.addEventListener('seeked', mark);
    heroLabel();
    hero.addEventListener('loadedmetadata', heroLabel);
    if (!canWatch) { if (!heroHeld && !isPhone()) heroPlay(); }
    else {
      new IntersectionObserver(function (entries) {
        entries.forEach(function (en) {
          if (en.isIntersecting && !heroHeld && !isPhone()) heroPlay();
          else if (!en.isIntersecting && !hero.paused) hero.pause();
        });
      }, { threshold: 0.35 }).observe(heroScreen);
    }
  }

  // Section entrances (M2): fired once, as each block comes 88% down the
  // screen. Pictures follow their heading; lists arrive row by row; bars grow.
  var reveal = '.tour-panel, .install-shot, .ways > div, .domains-shot, .domains-side, .cost-chart, .habits-table, .habits-shot, .limit-group, .faq-list details';
  doc.querySelectorAll(reveal).forEach(function (el, i) {
    el.setAttribute('data-reveal', '');
    var sib = el.parentNode ? Array.prototype.indexOf.call(el.parentNode.children, el) : 0;
    el.style.setProperty('--d', Math.min(sib, 3) * 80 + 'ms');
  });
  doc.querySelectorAll('.tabs').forEach(function (el) { el.setAttribute('data-reveal-list', ''); });
  var targets = doc.querySelectorAll('[data-reveal], .landing h2, [data-reveal-list], .bars');
  // A block is revealed once its top passes 88% of the screen height, and so
  // is everything above it, so a fast jump down the page never leaves
  // anything hidden behind the reader.
  if (!reduce) {
    var pending = Array.prototype.slice.call(targets);
    var queued = false;
    var check = function () {
      queued = false;
      // A viewport taller than any screen is a full-page capture: show it all.
      var line = window.innerHeight > 2400 ? Infinity : window.innerHeight * 0.88;
      pending = pending.filter(function (t) {
        if (t.getBoundingClientRect().top < line) { t.classList.add('is-in'); return false; }
        return true;
      });
      if (!pending.length) { window.removeEventListener('scroll', onScroll); window.removeEventListener('resize', onScroll); }
    };
    var onScroll = function () { if (!queued) { queued = true; requestAnimationFrame(check); } };
    window.addEventListener('scroll', onScroll, { passive: true });
    window.addEventListener('resize', onScroll);
    // Tabs and workload switches show new panels without a scroll.
    doc.addEventListener('click', function (ev) { if (ev.target.closest && ev.target.closest('[role="tab"]')) setTimeout(onScroll, 0); });
    check();
  } else {
    targets.forEach(function (t) { t.classList.add('is-in'); });
  }

  // Demo video (8.2): autoplay muted only without reduced motion and only in view.
  var video = doc.querySelector('.demo-video');
  doc.querySelectorAll('.chapter').forEach(function (c) {
    c.addEventListener('click', function () { if (!video) return; video.currentTime = Number(c.getAttribute('data-at')); var p = video.play(); if (p && p.catch) p.catch(function () {}); });
  });
  if (video && !reduce && canWatch) {
    var userPaused = false;
    video.addEventListener('pause', function () { if (!video.dataset.auto) userPaused = true; delete video.dataset.auto; });
    new IntersectionObserver(function (entries) {
      entries.forEach(function (en) {
        if (en.isIntersecting && !userPaused) { video.muted = true; var p = video.play(); if (p && p.catch) p.catch(function () {}); }
        else if (!en.isIntersecting && !video.paused) { video.dataset.auto = '1'; video.pause(); }
      });
    }, { threshold: 0.5 }).observe(video);
  }
})();
