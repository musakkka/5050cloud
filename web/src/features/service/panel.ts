// SPDX-License-Identifier: AGPL-3.0-only

/** Width of the side panel layer inside a canvas `containerW` px wide (matches `md:w-[min(903px,calc(100%-56px))]` in routes.tsx). */
export const PANEL_WIDTH = (containerW: number) => Math.min(903, containerW - 56);
