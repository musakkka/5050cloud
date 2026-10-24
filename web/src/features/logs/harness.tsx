// SPDX-License-Identifier: AGPL-3.0-only

// Dev-only harness: renders the Log Explorer alone against the mocks (not part of the build).
import React from "react";
import ReactDOM from "react-dom/client";
import "@fontsource-variable/inter/wght.css";
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/500.css";
import "@fontsource/jetbrains-mono/600.css";
import "../../index.css";
import { applyTheme } from "../../kit/theme";
import { Toaster } from "../../kit";
import LogExplorer from "./LogExplorer";

(async () => {
  applyTheme();
  const { startMocks } = await import("../../api/mocks/browser");
  await startMocks();
  ReactDOM.createRoot(document.getElementById("root")!).render(
    <React.StrictMode><div className="h-screen bg-background text-foreground"><LogExplorer environmentId="env-prod" /></div><Toaster /></React.StrictMode>,
  );
})();
