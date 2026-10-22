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

// checkShape returns problems with claims.json itself.
export function checkShape(claims, knownTests) {
  const problems = [];
  const ids = new Set();
  for (const c of claims.claims || []) {
    if (!c.id || !c.claim || !c.condition || !Array.isArray(c.evidence) || !c.evidence.length) problems.push(`claim ${c.id || '?'}: needs id, claim, evidence and condition`);
    if (ids.has(c.id)) problems.push(`claim ${c.id}: duplicate id`);
    ids.add(c.id);
    for (const e of c.evidence || []) {
      if (e.startsWith('source:')) { if (!/^source:https:\/\//.test(e)) problems.push(`claim ${c.id}: source must be an https URL`); continue; }
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
      if (e.startsWith('source:')) continue;
      if (!results.has(e)) problems.push(`claim ${c.id}: ${e} is not in the report`);
      else if (!results.get(e)) problems.push(`claim ${c.id}: ${e} failed`);
    }
  }
  return problems;
}
