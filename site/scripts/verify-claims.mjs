// SPDX-License-Identifier: MIT
// Checks claims.json against an e2e report: every test a claim cites must be
// present and passing. Usage: npm run verify-claims -- e2e/out/<ts>/report.json
import { readFileSync } from 'node:fs';
import { checkClaims } from '../src/lib/claims.mjs';

const path = process.argv[2];
if (!path) {
  console.error('usage: npm run verify-claims -- path/to/report.json');
  process.exit(2);
}
const claims = JSON.parse(readFileSync(new URL('../claims.json', import.meta.url), 'utf8'));
const report = JSON.parse(readFileSync(path, 'utf8'));
const problems = checkClaims(claims, report);
for (const p of problems) console.error(p);
if (problems.length) {
  console.error(`${problems.length} claim(s) are not backed by a passing test. Take them off the page or fix the tests.`);
  process.exit(1);
}
console.log(`All ${claims.claims.length} claims are backed by passing tests or cited sources.`);
