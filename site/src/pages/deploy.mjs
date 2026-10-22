// SPDX-License-Identifier: MIT
// /deploy: the target of the "Deploy on 5050cloud" README button. It asks for
// the visitor's console URL once, remembers it in this browser, and sends
// them to <console>/new/template/<id> with any other query parameters.

export function deployPage({ shell, rel }) {
  const main = `<main id="main" class="content narrow deploy">
<h1>Deploy on 5050cloud</h1>
<p id="what">Open a template in your own 5050cloud console.</p>
<form id="deploy-form" novalidate>
  <label for="console">Your console URL</label>
  <input id="console" name="console" type="url" inputmode="url" autocomplete="url" placeholder="https://console.example.com" required>
  <p class="small">Saved in this browser only, so the next button goes straight there.</p>
  <p id="err" class="err" role="alert" hidden></p>
  <button class="btn primary" type="submit">Continue</button>
</form>
<p class="small">No console yet? <a href="${rel('deploy', '/docs/quick-start')}">Install 5050cloud</a> on a VM with one command.</p>
<h2>Add the button to your README</h2>
<pre><code>[![Deploy on 5050cloud](https://5050cloud.com/button.svg)](https://5050cloud.com/deploy?template=TEMPLATE_ID)</code></pre>
<p class="small">See <a href="${rel('deploy', '/docs/guides/templates')}">Templates and the Deploy on 5050cloud button</a>.</p>
</main>
<script src="${rel('deploy', '/deploy.js')}" defer></script>`;
  return shell({ slug: 'deploy', title: 'Deploy on 5050cloud', description: 'Open a 5050cloud template in your own console.', main });
}
