// SPDX-License-Identifier: MIT
// cluster.cjs: run N copies of a Node app that share its port (5050cloud "Use all cores").
// Usage: node [flags] cluster.cjs <processes> <script> [args...]
'use strict';
const cluster = require('node:cluster');
const path = require('node:path');

const n = Math.max(1, Math.min(64, parseInt(process.argv[2], 10) || 1));
const script = process.argv[3];
if (!script) {
  console.error('cluster.cjs: usage: node cluster.cjs <processes> <script> [args...]');
  process.exit(2);
}
cluster.setupPrimary({ exec: path.resolve(script), args: process.argv.slice(4) });

const slots = new Map(); // worker id -> { slot, startedAt }
let stopping = false;
let exitCode = 0;

function fork(slot) {
  const w = cluster.fork({ CLUSTER_WORKER: String(slot) });
  slots.set(w.id, { slot, startedAt: Date.now() });
}

cluster.on('exit', (w, code, signal) => {
  const info = slots.get(w.id);
  slots.delete(w.id);
  if (code) exitCode = code;
  if (stopping) {
    if (slots.size === 0) process.exit(exitCode);
    return;
  }
  // Restart in the same slot; wait longer when it died soon after starting so a crash loop never spins.
  const early = Date.now() - info.startedAt < 10000;
  console.error(`cluster.cjs: process ${info.slot} exited (${signal || code}); restarting in ${early ? 5 : 0.5} s`);
  setTimeout(() => { if (!stopping) fork(info.slot); }, early ? 5000 : 500);
});

function stop(sig) {
  if (stopping) return;
  stopping = true;
  for (const w of Object.values(cluster.workers)) w.process.kill(sig);
  // Hard stop inside the 30 s drain window.
  setTimeout(() => process.exit(exitCode || 1), 28000).unref();
  if (slots.size === 0) process.exit(exitCode);
}
process.on('SIGTERM', () => stop('SIGTERM'));
process.on('SIGINT', () => stop('SIGINT'));

for (let i = 1; i <= n; i++) fork(i);
