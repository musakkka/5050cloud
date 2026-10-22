// SPDX-License-Identifier: MIT
(function () {
  document.querySelectorAll('.code .copy').forEach(function (b) {
    b.addEventListener('click', function () {
      var code = b.parentNode.querySelector('code').textContent;
      var done = function () { b.textContent = 'Copied'; setTimeout(function () { b.textContent = 'Copy'; }, 1500); };
      if (navigator.clipboard) navigator.clipboard.writeText(code).then(done, function () {});
    });
  });
  var t = document.querySelector('.navtoggle');
  var s = document.getElementById('sidebar');
  if (t && s) t.addEventListener('click', function () {
    var open = s.classList.toggle('open');
    t.setAttribute('aria-expanded', open ? 'true' : 'false');
  });
})();
