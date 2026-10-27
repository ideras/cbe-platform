import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';

// The UI tests run against tests/fake-api.js, so they cannot notice a Go method that src/api.js forgets to pass through:
// in the built application that call is undefined. Every api.X the components use must be exported by both.
const source = name => readFileSync(new URL(name, import.meta.url), 'utf8');
// api.Name( is required — the host must offer it; api.Name?.( is optional — a host may lack it, like the
// student's session-logging calls that the preview host has no use for.
const used = new Set(readdirSync(new URL('../src/', import.meta.url)).filter(name => /\.(svelte|js)$/.test(name))
  .flatMap(name => [...source(`../src/${name}`).matchAll(/\bapi\.([A-Za-z]+)\(/g)].map(match => match[1])));
// The calculator is a shared component (shared/theme/calculator) that takes the host's api as a prop: its
// calls must be offered by this app's exports and stand-in too.
const shared = '../../../../shared/theme/calculator';
for (const name of readdirSync(new URL(`${shared}/`, import.meta.url)).filter(name => /\.(svelte|js)$/.test(name))) {
  for (const match of source(`${shared}/${name}`).matchAll(/\bapi\.([A-Za-z]+)\(/g)) used.add(match[1]);
}
const api = source('../src/api.js');
const real = new Set([...api.match(/export\s*{([^}]*)}/)[1].split(',').map(name => name.trim()).filter(Boolean),
  ...[...api.matchAll(/export function ([A-Za-z]+)/g)].map(match => match[1])]);
const bindings = source('../bindings/exam-platform/apps/student/internal/client/service.js');

test('every API call the student application makes is passed through to Go', () => {
  for (const name of used) {
    assert.ok(real.has(name), `src/api.js does not export ${name}`);
    if (!/^On[A-Z]/.test(name)) assert.match(bindings, new RegExp(`export function ${name}\\b`), `the generated bindings have no ${name}`);
  }
});

test('the test stand-in offers the same calls', () => {
  const fake = source('./fake-api.js');
  for (const name of used) assert.match(fake, new RegExp(`export (async )?function ${name}\\b`), `tests/fake-api.js has no ${name}`);
});
