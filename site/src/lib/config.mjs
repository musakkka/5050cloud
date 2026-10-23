// SPDX-License-Identifier: MIT
// One place for the URLs and commands that appear on many pages. Markdown
// pages use them as {{NAME}} placeholders (see expand below), so a change of
// repository or install host is a one-line edit here.

export const ORIGIN = 'https://5050cloud.com';
// The public repository. FIFTY_SITE_REPO overrides it at build time.
export const REPO = (process.env.FIFTY_SITE_REPO || 'https://github.com/musakkka/5050cloud').replace(/\/$/, '');
// The site serves the installer at /install.sh (build.mjs copies install/install.sh).
export const GET = `${ORIGIN}/install.sh`;

// Every install command on the site uses 5050cloud.com/install.sh and nothing else.
export const INSTALL = `curl -fsSL ${GET} | sudo sh`;
export const INSTALL_WGET = `wget -qO- ${GET} | sudo sh`;
export const INSTALL_SUDO = INSTALL;
export const INSTALL_GUIDED = `curl -fsSLo install.sh ${GET} && sudo sh install.sh`;
export const INSTALL_CLI = `curl -fsSL ${GET} | sh -s -- --cli`;

export const VARS = {
  ORIGIN, REPO, GET, INSTALL, INSTALL_WGET, INSTALL_SUDO, INSTALL_GUIDED, INSTALL_CLI,
};

// expand replaces {{NAME}} with the value above. An unknown name is an error,
// so a typo cannot reach a published page.
export function expand(text, where = 'text') {
  return text.replace(/(?<!\$)\{\{([A-Z_]+)\}\}/g, (m, k) => {
    if (!(k in VARS)) throw new Error(`${where}: unknown placeholder ${m}`);
    return VARS[k];
  });
}
