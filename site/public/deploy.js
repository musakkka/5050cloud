// SPDX-License-Identifier: MIT
(function () {
  var KEY = '5050cloud.console';
  var params = new URLSearchParams(location.search);
  var template = params.get('template') || '';
  var form = document.getElementById('deploy-form');
  var input = document.getElementById('console');
  var err = document.getElementById('err');
  var what = document.getElementById('what');
  var validId = /^[a-z0-9][a-z0-9-]{0,63}$/.test(template);
  if (!validId) {
    what.textContent = template ? 'That template name is not valid.' : 'This page opens a template in your own console. The link needs ?template=<id>.';
  } else {
    what.textContent = 'Open the ' + template + ' template in your own 5050cloud console.';
  }
  function normalise(v) {
    v = (v || '').trim();
    if (!v) return null;
    if (!/^https?:\/\//i.test(v)) v = 'https://' + v;
    try {
      var u = new URL(v);
      if (u.protocol !== 'https:' && u.hostname !== 'localhost' && u.hostname !== '127.0.0.1') return null;
      return u.origin;
    } catch (e) { return null; }
  }
  function target(origin) {
    var q = new URLSearchParams(params);
    q.delete('template');
    var s = q.toString();
    return origin + '/new/template/' + encodeURIComponent(template) + (s ? '?' + s : '');
  }
  var saved = null;
  try { saved = localStorage.getItem(KEY); } catch (e) {}
  if (saved && validId && normalise(saved) && !params.has('change')) {
    location.replace(target(normalise(saved)));
    return;
  }
  if (saved) input.value = saved;
  form.addEventListener('submit', function (ev) {
    ev.preventDefault();
    var origin = normalise(input.value);
    if (!origin) { err.hidden = false; err.textContent = 'Enter the https address of your console, for example https://console.example.com.'; return; }
    if (!validId) { err.hidden = false; err.textContent = 'This link does not name a template.'; return; }
    try { localStorage.setItem(KEY, origin); } catch (e) {}
    location.assign(target(origin));
  });
})();
