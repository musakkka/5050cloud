// SPDX-License-Identifier: AGPL-3.0-only

/** Shown for a console page that is not part of this build. Overlays render nothing so the canvas stays usable. */
export function NotBuilt({ overlay }: { overlay?: boolean }) {
  if (overlay) return null;
  return <div className="p-8 text-sm text-gray-500">This page is not part of this build yet.</div>;
}
