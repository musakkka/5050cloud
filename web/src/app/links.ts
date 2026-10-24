// SPDX-License-Identifier: AGPL-3.0-only

// Project links shared by the shell and the logged-out screens. Kept in a module with no
// imports so the session -> auth -> shell import cycle never sees them undefined.
export const REPO_URL = "https://github.com/musakkka/5050cloud";
export const LINKS = {
  docs: `${REPO_URL}/tree/main/docs`,
  community: `${REPO_URL}/discussions`,
  support: `${REPO_URL}/issues`,
};
