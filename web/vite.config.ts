// SPDX-License-Identifier: AGPL-3.0-only

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// `npm run dev` proxies API calls to a local 5050d (`5050d serve --dev`, port 5050).
// Set FIFTY_API to point at another control plane, for example http://127.0.0.1:5050.
export default defineConfig({
  plugins: [react()],
  test: { include: ["src/**/*.test.{ts,tsx}"] },
  server: { port: 5173, proxy: { "/api": { target: process.env.FIFTY_API || "http://127.0.0.1:5050", changeOrigin: true } } },
});
