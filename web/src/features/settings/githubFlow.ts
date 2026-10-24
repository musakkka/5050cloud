// SPDX-License-Identifier: AGPL-3.0-only

// The GitHub App setup is two trips to GitHub: create the app (manifest flow),
// then install it on an account. The callback of the first trip lands back in
// the console with `?install=<url>`; we continue straight to the install page
// and remember where the person started, because GitHub's post-install
// redirect always goes to /settings/github?installed=1.

const RETURN_KEY = "5050.github.return";

function store(): Storage | undefined {
  try { return window.sessionStorage; } catch { return undefined; }
}

/** Starts the install trip and remembers `returnTo` for the way back. */
export function goInstall(installUrl: string, returnTo = location.pathname) {
  store()?.setItem(RETURN_KEY, returnTo);
  location.assign(installUrl);
}

/**
 * After the app-creation callback: forwards to GitHub's install page and
 * returns true. Call it on mount of every page the callback can land on.
 */
export function continueAfterCreate(): boolean {
  const q = new URLSearchParams(location.search);
  const install = q.get("install");
  if (!install || !/^https:\/\/github\.com\//.test(install)) return false;
  goInstall(install, location.pathname);
  return true;
}

/** After the install redirect: the page to go back to, once. */
export function takeReturn(): string | undefined {
  const q = new URLSearchParams(location.search);
  if (q.get("installed") !== "1") return undefined;
  const s = store();
  const to = s?.getItem(RETURN_KEY) || undefined;
  s?.removeItem(RETURN_KEY);
  return to && to.startsWith("/") && !to.startsWith("/settings/github") ? to : undefined;
}
