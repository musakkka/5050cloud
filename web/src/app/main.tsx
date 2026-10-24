// SPDX-License-Identifier: AGPL-3.0-only

// Console entry: fonts, styles, the optional MSW mocks, then the app.
import React from "react";
import ReactDOM from "react-dom/client";
import "@fontsource-variable/inter/wght.css";
import "@fontsource/ibm-plex-serif/400.css";
import "@fontsource/ibm-plex-serif/500.css";
import "@fontsource/ibm-plex-serif/600.css";
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/500.css";
import "@fontsource/jetbrains-mono/600.css";
import "@xyflow/react/dist/style.css";
import "../index.css";
import { applyTheme } from "../kit/theme";
import { readPref } from "../kit";
import App from "./App";

export async function boot() {
  applyTheme();
  document.documentElement.toggleAttribute("data-reduce-motion", !!readPref<{ reduceMotion?: boolean }>("5050.flags", {}).reduceMotion);
  // `npm run dev:mocks` answers every API call from the generated MSW fixtures.
  if (import.meta.env.VITE_MOCKS === "1") {
    const { startMocks } = await import("../api/mocks/browser");
    await startMocks();
  }
  ReactDOM.createRoot(document.getElementById("root")!).render(<React.StrictMode><App /></React.StrictMode>);
}
