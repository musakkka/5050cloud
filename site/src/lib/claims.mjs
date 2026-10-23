// SPDX-License-Identifier: MIT
// Claim checks shared by the tests and scripts/verify-claims.mjs.

export function reportResults(report) {
  const list = Array.isArray(report) ? report : report.tests || report.results || [];
  const out = new Map();
  for (const t of list) {
    const id = t.id || t.test;
    const pass = t.pass === true || t.status === 'pass' || t.result === 'pass' || t.passed === true;
    if (id) out.set(id, pass);
  }
  return out;
}

// Evidence that is a file in this repository rather than an e2e test: a real
// recording or screenshot (recording:public/media/...), a measurement
// (bench:data/benchmark.json) or a dated price file (data:data/pricing.json).
export const isArtifact = (e) => /^(recording|bench|data):/.test(e);

// checkShape returns problems with claims.json itself.
export function checkShape(claims, knownTests) {
  const problems = [];
  const ids = new Set();
  for (const c of claims.claims || []) {
    if (!c.id || !c.claim || !c.condition || !Array.isArray(c.evidence) || !c.evidence.length) problems.push(`claim ${c.id || '?'}: needs id, claim, evidence and condition`);
    if (ids.has(c.id)) problems.push(`claim ${c.id}: duplicate id`);
    if (c.fallback && !fallbackOk(c.fallback)) problems.push(`claim ${c.id}: the fallback states a figure or a guarantee`);
    ids.add(c.id);
    for (const e of c.evidence || []) {
      if (e.startsWith('source:')) { if (!/^source:https:\/\//.test(e)) problems.push(`claim ${c.id}: source must be an https URL`); continue; }
      if (isArtifact(e)) continue;
      if (knownTests && !knownTests.has(e)) problems.push(`claim ${c.id}: ${e} is not a test id in the spec`);
    }
  }
  return problems;
}

// checkClaims returns a problem for every cited test that is missing or failing.
export function checkClaims(claims, report) {
  const results = reportResults(report);
  const problems = [];
  for (const c of claims.claims) {
    for (const e of c.evidence) {
      if (e.startsWith('source:') || isArtifact(e)) continue;
      if (!results.has(e)) problems.push(`claim ${c.id}: ${e} is not in the report`);
      else if (!results.get(e)) problems.push(`claim ${c.id}: ${e} failed`);
    }
  }
  return problems;
}

// proven returns the ids of the claims the landing page may state: every
// cited test is in the report and passing. Claims backed only by sources
// (prices) are always proven. With no report, no test-backed claim is.
export function proven(claims, report) {
  const results = report ? reportResults(report) : new Map();
  const ok = new Set();
  for (const c of claims.claims) {
    const tests = c.evidence.filter((e) => !e.startsWith('source:') && !isArtifact(e));
    if (tests.every((t) => results.get(t) === true)) ok.add(c.id);
  }
  return ok;
}

// A fallback names the feature only: no figure, time or guarantee.
export const FALLBACK_BANNED = /\d|\b(zero|never|under|always|keeps?|guarantee[sd]?|instant|seconds|minutes?|downtime)\b/i;
export const fallbackOk = (text) => !FALLBACK_BANNED.test(text.replace(/\b5050\b/g, ''));
