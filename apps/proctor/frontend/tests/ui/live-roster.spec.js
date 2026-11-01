import { test, expect } from '@playwright/test';

const observed = '2035-01-01T22:23:23Z';
const missing = [{ student_name: 'Grace Hopper', account_number: '005678' }];
const makeRow = (index, name, account, extra = {}) => ({
  session_id: `${String(index + 1).padStart(8, '0')}-0000-4000-8000-000000000000`, exam_id: '11111111-1111-4111-8111-111111111111',
  student_name: name, account_number: account, roster_active: true, status: 'active', persisted_status: 'active',
  seconds_remaining: 1122, finalization_pending: false, final_version: null, locked: null, network_warning: false, focus_warning: false,
  started_at: '2035-01-01T22:00:00Z', deadline_at: '2035-01-01T22:42:05Z', grace_until: '2035-01-01T22:42:35Z',
  last_known_unfocused_duration_ms: null,
  presence: { monitoring: true, connectivity: 'healthy', hostname: `lab-pc-${index}`, remote_addr: `10.0.0.${index + 20}`,
    heartbeat_age_ms: 3100, missed_intervals: 0, volume_percent: 85, volume_muted: false, ever_overdue: false,
    focus: { state: 'focused', unfocused_duration_ms: 0 } }, ...extra,
});
function fixture() {
  const ada = makeRow(0, 'Ada Lovelace', '001234', { status: 'submitted', persisted_status: 'submitted', seconds_remaining: 0,
    final_version: { final_version_id: 4128, version_seq: 1, reason: 'grace_expired' } });
  ada.presence = { ...ada.presence, monitoring: false, connectivity: 'terminal', heartbeat_age_ms: 14700, volume_percent: 100, focus: { state: 'unknown', unfocused_duration_ms: null } };
  const alan = makeRow(1, 'Alan Turing', '002468');
  const barbara = makeRow(2, 'Barbara Liskov', '003579', { network_warning: true, focus_warning: true, seconds_remaining: 250 });
  barbara.presence = { ...barbara.presence, connectivity: 'overdue', heartbeat_age_ms: 35000, missed_intervals: 1,
    volume_percent: 80, focus: { state: 'unfocused', unfocused_duration_ms: 97000 } };
  const dennis = makeRow(3, 'Dennis Ritchie', '004680', { locked: { since: '2035-01-01T22:18:11Z', reason: 'no_contact' }, network_warning: true, seconds_remaining: 725, last_known_unfocused_duration_ms: 2000 });
  dennis.presence = { ...dennis.presence, connectivity: 'overdue', heartbeat_age_ms: 312000, missed_intervals: 20,
    volume_percent: null, volume_muted: null, focus: { state: 'unknown', unfocused_duration_ms: null } };
  return [ada, alan, barbara, dennis];
}
const rows = page => page.locator('tbody tr[data-session-id]');
const filters = page => page.getByRole('group', { name: 'Filter sessions' });
const count = (page, name) => page.locator(`[data-stat="${name}"] .num`);

async function monitor(page, sessions = fixture(), students = missing) {
  await page.goto('/#/monitoring');
  await page.getByRole('table', { name: /All sessions/ }).waitFor();
  await page.evaluate(({ sessions, students, observed }) => window.ProctorHarness.monitoringRoster(sessions, students, observed), { sessions, students, observed });
  await expect(count(page, 'total')).toHaveText(String(sessions.length));
}

test('summary counts are unfiltered, warnings remain independent, and filters survive polling', async ({ page }) => {
  await monitor(page);
  for (const [name, total] of [['total', 4], ['progress', 3], ['attention', 2], ['locked', 1], ['submitted', 1]]) await expect(count(page, name)).toHaveText(String(total));
  await expect(rows(page).first()).toContainText('Dennis Ritchie');
  const barbara = rows(page).filter({ hasText: 'Barbara Liskov' });
  await expect(barbara.locator('.network')).toContainText('Network overdue');
  await expect(barbara.locator('.focus')).toContainText('Sustained focus loss');
  await expect(barbara.locator('.remaining')).toHaveClass(/low/);
  await filters(page).getByRole('button', { name: 'Needs attention', exact: true }).click();
  await expect(rows(page)).toHaveCount(2);
  await page.waitForTimeout(1100);
  await expect(filters(page).getByRole('button', { name: 'Needs attention', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await expect(rows(page)).toHaveCount(2);
  await expect(count(page, 'total')).toHaveText('4');
  await filters(page).getByRole('button', { name: 'In progress', exact: true }).click();
  await expect(rows(page)).toHaveCount(3);
  await expect(rows(page).first()).toContainText('Dennis Ritchie');
  await filters(page).getByRole('button', { name: 'Submitted', exact: true }).click();
  await expect(rows(page)).toHaveCount(1);
  await expect(rows(page).first()).toContainText('Ada Lovelace');
  await expect(rows(page).first()).toContainText('Final #4128 · sequence 1 · grace_expired');
  await expect(rows(page).first().getByRole('button')).toHaveCount(1);
  await rows(page).first().getByRole('button', { name: 'Review 001234', exact: true }).click();
  expect(await page.evaluate(() => window.ProctorHarness.calls.windows.at(-1).name)).toMatch(/^review-/);
  await filters(page).getByRole('button', { name: 'All', exact: true }).click();
  await expect(rows(page)).toHaveCount(4);
});

test('stale data stops the live indicator and retains server timing and lock age', async ({ page }) => {
  await monitor(page);
  await expect(page.locator('.pulse.running')).toHaveCount(1);
  const timing = await rows(page).first().locator('.remaining').textContent();
  const lock = rows(page).first().getByText('Locked 5 min ago · time keeps running', { exact: true });
  await expect(lock).toBeVisible();
  await page.evaluate(() => window.ProctorHarness.stale(true));
  await expect(page.getByRole('alert')).toContainText('historical observations');
  await expect(page.locator('.live')).toContainText('Last confirmed');
  await expect(page.locator('.pulse.running')).toHaveCount(0);
  await page.waitForTimeout(1200);
  await expect(rows(page).first().locator('.remaining')).toHaveText(timing);
  await expect(lock).toBeVisible();
  await page.evaluate(() => window.ProctorHarness.stale(false));
  await expect(page.locator('.pulse.running')).toHaveCount(1);
});

test('empty filtered and initial rosters are explicit; missing students use labelled pills', async ({ page }) => {
  await monitor(page, [fixture()[0]]);
  await expect(page.getByRole('list', { name: 'Students without a session' }).getByRole('listitem')).toHaveCount(1);
  await expect(page.getByRole('listitem')).toContainText('Grace Hopper');
  await expect(page.getByRole('listitem')).toContainText('005678');
  await expect(page.locator('.avatar')).toHaveAttribute('aria-hidden', 'true');
  await filters(page).getByRole('button', { name: 'In progress', exact: true }).click();
  await expect(rows(page)).toHaveCount(0);
  await expect(page.getByRole('cell', { name: 'No sessions match this filter.', exact: true })).toBeVisible();
  await page.evaluate(observed => window.ProctorHarness.monitoringRoster([], [], observed), observed);
  await expect(count(page, 'total')).toHaveText('0');
  await expect(page.getByRole('cell', { name: 'No sessions for this exam yet.', exact: true })).toBeVisible();
  await expect(page.getByText('No students without a session.', { exact: true })).toBeVisible();
});

test('pending finalization needs attention but is not counted as a finished submission', async ({ page }) => {
  const sessions = fixture();
  sessions[1] = { ...sessions[1], status: 'submitted', finalization_pending: true, seconds_remaining: 0 };
  await monitor(page, sessions);
  await expect(count(page, 'attention')).toHaveText('3');
  await expect(count(page, 'submitted')).toHaveText('1');
  const pending = rows(page).filter({ hasText: 'Alan Turing' });
  await expect(pending).toContainText('Finalization pending — no confirmed final yet');
  await expect(pending.getByRole('button', { name: /Extend grace/ })).toHaveCount(0);
  await expect(pending.getByRole('button', { name: 'Force-submit 002468…', exact: true })).toBeVisible();
});

test('a lock outside the selected filter still updates the count and sounds once', async ({ page }) => {
  await page.addInitScript(() => {
    window.__plays = 0;
    HTMLMediaElement.prototype.play = function () { window.__plays++; return Promise.resolve(); };
  });
  await monitor(page);
  await expect.poll(() => page.evaluate(() => window.__plays)).toBe(1);
  await filters(page).getByRole('button', { name: 'Submitted', exact: true }).click();
  await page.evaluate(() => window.ProctorHarness.lock(1, 'away'));
  await expect(count(page, 'locked')).toHaveText('2');
  await expect.poll(() => page.evaluate(() => window.__plays)).toBe(2);
  await expect(rows(page)).toHaveCount(1);
  await expect(rows(page).first()).toContainText('Ada Lovelace');
  await page.waitForTimeout(600);
  expect(await page.evaluate(() => window.__plays)).toBe(2);
});

for (const width of [1280, 600, 390]) {
  test(`mock monitoring layout fits at ${width}px with horizontal scrolling confined to the table`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 1100 });
    await monitor(page);
    await expect(page.locator('.roster-head')).toHaveCSS('background-image', /linear-gradient/);
    await expect(page.locator('.missing-head')).toHaveCSS('background-image', /linear-gradient/);
    await expect(page.getByRole('button', { name: 'Extend grace for everyone…', exact: true })).toHaveClass(/primary/);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    if (width < 1000) {
      const scroll = page.getByRole('region', { name: 'Session table — scroll horizontally for controls', exact: true });
      expect(await scroll.evaluate(el => el.scrollWidth > el.clientWidth)).toBe(true);
      await scroll.focus(); await page.keyboard.press('ArrowRight');
      await expect.poll(() => scroll.evaluate(el => el.scrollLeft)).toBeGreaterThan(0);
      await scroll.evaluate(el => el.scrollLeft = 0);
    }
    await page.screenshot({ path: info.outputPath('live-roster.png'), fullPage: true });
    // Every existing control remains available in a narrow window.
    await page.getByRole('button', { name: 'Unlock 004680…', exact: true }).click();
    await expect(page.getByRole('dialog', { name: 'Unlock exam', exact: true })).toBeVisible();
    await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  });
}
