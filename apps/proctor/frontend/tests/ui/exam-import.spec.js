import { test, expect } from '@playwright/test';
import { confirmImportKind, connect, calls } from './helpers.js';

async function openImport(page, { longFile = false, scheduled = false, withCalculator = true } = {}) {
  await connect(page, { openClass: 'Mathematics 10A' });
  await page.evaluate(({ longFile, scheduled, withCalculator }) => window.ProctorHarness.examFile({
    file: longFile ? `${'calculator-test-'.repeat(12)}exam.md` : 'calculator-test-exam.md',
    valid: true, title: 'Prueba manual de calculadora', exercise_count: 2, slot_count: 2,
    navigation: 'free', duration_seconds: 1800, grace_seconds: 30, min_volume_percent: 80,
    calculator: withCalculator ? { modes: ['scientific'], angle: 'user', disabled: 0 } : null,
    opens_at: scheduled ? '2026-01-01T08:00:00Z' : null,
    closes_at: scheduled ? '2026-01-01T10:00:00Z' : null,
  }), { longFile, scheduled, withCalculator });
  await page.getByRole('button', { name: 'Import exam…' }).click();
  await confirmImportKind(page);
  return page.locator('.import-stack');
}

test('one picker detects the kind, requires confirmation, and respects overrides in both directions', async ({ page }) => {
  await connect(page, { openClass: 'Mathematics 10A' });
  const panel = page.getByRole('region', { name: 'Exams', exact: true });
  const picker = panel.getByRole('button', { name: /^Import .*…$/ });
  const dialog = page.getByRole('dialog', { name: 'Confirm the exam kind' });
  const workspaceKind = dialog.getByRole('radio', { name: 'Workspace Exam' });
  const traditionalKind = dialog.getByRole('radio', { name: 'Traditional Exam' });
  const override = dialog.locator('[aria-live="polite"]');
  await expect(picker).toHaveCount(1);

  await page.evaluate(() => window.ProctorHarness.examFile({ file: 'exam.md', valid: true }));
  await picker.click();
  await expect(dialog).toContainText('Selected file');
  await expect(dialog.getByText('exam.md', { exact: true })).toBeVisible();
  await expect(dialog).toContainText('Detected: Traditional Exam');
  await expect(dialog.getByRole('radio')).toHaveCount(2);
  await expect(traditionalKind).toBeChecked();
  await expect(dialog.getByRole('button', { name: 'Validate as Traditional Exam' })).toBeFocused();
  await expect(override).toBeEmpty();
  await expect(panel.getByLabel('Exam PIN')).toHaveCount(0);
  await traditionalKind.press('ArrowLeft');
  await expect(workspaceKind).toBeChecked();
  await expect(override).toContainText('selected file is a Workspace Exam');
  await dialog.getByRole('button', { name: 'Validate as Workspace Exam' }).click();
  const workspace = page.getByTestId('workspace-import');
  await expect(workspace).toContainText('A Workspace Exam requires the package’s workspace.md.');
  await expect(workspace.getByLabel('Exam PIN')).toHaveCount(0);

  await page.evaluate(() => window.ProctorHarness.workspacePackage({ folder: 'lab', valid: true }));
  await workspace.getByRole('button', { name: /choose/i }).click();
  await expect(dialog.getByText('workspace.md', { exact: true })).toBeVisible();
  await expect(dialog).toContainText('Detected: Workspace Exam');
  await expect(workspaceKind).toBeChecked();
  await expect(override).toBeEmpty();
  await dialog.getByText('Optional calculator', { exact: true }).click();
  await expect(traditionalKind).toBeChecked();
  await expect(dialog).toContainText('Detected: Workspace Exam');
  await expect(override).toContainText('selected file is a Traditional Exam');
  await dialog.getByRole('button', { name: 'Validate as Traditional Exam' }).click();
  await expect(panel).toContainText('No exercises found in the traditional exam file.');
  await expect(panel.getByLabel('Exam PIN')).toHaveCount(0);
  expect(await calls(page)).toMatchObject({ chooseExam: 2, importExam: 0, workspaceImport: { imported: [] } });
});

test('cancelling the kind step before or during validation cannot open a stale preview', async ({ page }) => {
  await connect(page, { openClass: 'Mathematics 10A' });
  const picker = page.getByRole('button', { name: 'Import exam…' });
  const dialog = page.getByRole('dialog', { name: 'Confirm the exam kind' });
  const queueFile = () => page.evaluate(() => window.ProctorHarness.examFile({ file: 'midterm.md', valid: true, title: 'Midterm', exercise_count: 1, slot_count: 1, duration_seconds: 600 }));
  await queueFile();
  await picker.click();
  await expect(dialog).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(picker).toBeFocused();
  await expect(page.getByLabel('Exam PIN')).toHaveCount(0);

  await queueFile();
  await page.evaluate(() => window.ProctorHarness.holdImportValidation());
  await picker.click();
  await confirmImportKind(page);
  await expect(dialog.getByRole('button', { name: 'Validating…' })).toBeDisabled();
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.evaluate(() => window.ProctorHarness.releaseImportValidation());
  await expect(picker).toBeEnabled();
  await expect(dialog).toBeHidden();
  await expect(page.getByLabel('Exam PIN')).toHaveCount(0);
  expect(await calls(page)).toMatchObject({ importExam: 0, discardImport: 2 });
});

test('the kind dialog hugs its content on a tall desktop window', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 850 });
  await connect(page, { openClass: 'Mathematics 10A' });
  await page.evaluate(() => window.ProctorHarness.examFile({ file: 'midterm.md', valid: true }));
  await page.getByRole('button', { name: 'Import exam…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Confirm the exam kind' });
  const progress = dialog.getByRole('list').filter({ hasText: 'Validate file' });
  const footer = dialog.locator('footer');
  const checkGap = async () => {
    const endOfProgress = await progress.boundingBox();
    const actions = await footer.boundingBox();
    const gap = actions.y - endOfProgress.y - endOfProgress.height;
    expect(gap).toBeGreaterThanOrEqual(0);
    expect(gap).toBeLessThanOrEqual(24);
  };
  await expect(dialog.getByRole('button', { name: 'Validate as Traditional Exam' })).toBeEnabled();
  await checkGap();
  await dialog.getByRole('radio', { name: 'Workspace Exam' }).check();
  await expect(dialog.getByText(/This differs from the detected kind/)).toBeVisible();
  await checkGap();
});

test('the kind dialog stacks cards on narrow windows and keeps long filenames inside', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 620 });
  await connect(page, { openClass: 'Mathematics 10A' });
  const file = `${'long-exam-filename-'.repeat(10)}.md`;
  await page.evaluate(file => window.ProctorHarness.examFile({ file, valid: true }), file);
  await page.getByRole('button', { name: 'Import exam…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Confirm the exam kind' });
  await expect(dialog.getByText(file, { exact: true })).toBeVisible();
  const [workspace, traditional] = await dialog.locator('label.kind').evaluateAll(cards => cards.map(card => card.getBoundingClientRect().toJSON()));
  expect(traditional.top).toBeGreaterThan(workspace.bottom);
  expect(traditional.left).toBe(workspace.left);
  expect(await dialog.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true);
  await dialog.getByRole('button', { name: 'Cancel import', exact: true }).click();
  await expect(dialog).toBeHidden();
});

async function paint(locator, property) { return locator.evaluate((el, prop) => getComputedStyle(el)[prop], property); }
async function token(page, name) {
  return page.evaluate(name => {
    const probe = document.createElement('span'); probe.style.color = `var(--${name})`;
    document.body.append(probe); const color = getComputedStyle(probe).color; probe.remove(); return color;
  }, name);
}

test('PIN, navigation flag and preview dialog are accessible, keyboard operable and preserve the typed PIN', async ({ page }) => {
  const stack = await openImport(page);
  const file = stack.getByRole('region', { name: 'Exam file', exact: true });
  const access = stack.getByRole('region', { name: 'Exam access', exact: true });
  await expect(file.getByText('Validated', { exact: true })).toBeVisible();
  await expect(access.getByRole('note')).toContainText('Importing stores the exam in Mathematics 10A. Students cannot log in until you publish it.');
  await expect(file.getByRole('term')).toHaveText(['Title', 'Contents', 'Time', 'Sound', 'Calculator', 'Logins']);
  await expect(file.getByRole('definition').last()).toHaveText('Allowed any time');
  const pin = access.getByLabel('Exam PIN', { exact: true });
  await expect(pin).toHaveAccessibleDescription('Students log in with it. It is not stored in the file.');
  const submit = access.getByRole('button', { name: 'Import exam', exact: true });
  await pin.fill('   ');
  await expect(submit).toBeDisabled();
  await expect(access.getByText('Enter a PIN to continue')).toBeVisible();
  await pin.press('Enter');
  expect((await calls(page)).importExam).toBe(0);
  await pin.fill('  p-42  ');
  await expect(submit).toBeEnabled();
  await expect(access.getByText('Enter a PIN to continue')).toHaveCount(0);
  const toggle = access.locator('.icon-btn');
  await expect(toggle).toHaveAccessibleName('Show PIN');
  await expect(toggle).toHaveAttribute('title', 'Show PIN');
  await expect(toggle).toHaveAttribute('aria-pressed', 'false');
  const eye = await toggle.locator('svg').innerHTML();
  await pin.press('Tab');
  await expect(toggle).toBeFocused();
  await expect(toggle).toHaveCSS('outline-width', '2px');
  await toggle.press('Space');
  await expect(pin).toHaveAttribute('type', 'text');
  await expect(toggle).toHaveAttribute('aria-label', 'Hide PIN');
  await expect(toggle).toHaveAttribute('title', 'Hide PIN');
  await expect(toggle).toHaveAttribute('aria-pressed', 'true');
  expect(await toggle.locator('svg').innerHTML()).not.toBe(eye);
  await toggle.press('Space');
  await expect(pin).toHaveAttribute('type', 'password');
  await expect(pin).toHaveValue('  p-42  ');
  const flag = access.getByRole('checkbox');
  await expect(flag).not.toBeChecked();
  const helper = access.getByText('Applies to everyone and cannot be changed after the exam is published.');
  const neutral = await paint(helper, 'color');
  await helper.click(); // The whole label, including the helper, is clickable.
  await expect(flag).toBeChecked();
  await expect(helper).toHaveCSS('color', await token(page, 'alert-text'));
  await expect(helper).toHaveCSS('font-weight', '600');
  await flag.focus();
  await flag.press('Space');
  await expect(helper).toHaveCSS('color', neutral);
  await flag.check();
  const previewButton = file.getByRole('button', { name: 'Preview the exam', exact: true });
  await expect(previewButton).toHaveAttribute('aria-haspopup', 'dialog');
  await expect(previewButton.locator('.preview-icon svg')).toBeVisible();
  await expect(previewButton.locator('.preview-icon svg')).toHaveAttribute('aria-hidden', 'true');
  await previewButton.focus();
  await previewButton.press('Enter');
  const dialog = page.getByRole('dialog', { name: 'Prueba manual de calculadora', exact: true });
  await expect(dialog).toBeVisible();
  await expect(dialog.locator('.exam-preview')).toBeVisible();
  await expect(file.locator('.exam-preview')).toHaveCount(0);
  await expect(dialog).toContainText('You can try this exam’s calculator after importing it.');
  await expect(dialog.getByRole('button', { name: 'Open the calculator' })).toHaveCount(0);
  // Tab stays in the shared modal; Escape closes only the preview and restores its opener.
  await dialog.getByRole('button', { name: 'Close', exact: true }).focus();
  await page.keyboard.press('Tab');
  expect(await dialog.evaluate(el => el.contains(document.activeElement))).toBe(true);
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(previewButton).toBeFocused();
  await expect(pin).toHaveValue('  p-42  ');
  await expect(flag).toBeChecked();
  expect(await calls(page)).toMatchObject({ importExam: 0, discardImport: 0 });
  // Reopening does not contact the stored-exam API or consume the pending import.
  await previewButton.click();
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  expect(await page.evaluate(() => window.ProctorHarness.nativeCalls)).toEqual([]);
  await flag.uncheck();
  await pin.press('Enter');
  expect((await calls(page)).imported).toEqual([{ classId: '44444444-4444-4444-8444-444444444444', pin: '  p-42  ', forwardOnly: false }]);
});

for (const width of [1000, 400]) {
  test(`compact cards fit at ${width}px, including long filenames`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 });
    const stack = await openImport(page, { longFile: true });
    const headers = stack.locator('.card-header');
    const input = stack.getByLabel('Exam PIN', { exact: true });
    await expect(input).toHaveCSS('height', '32px');
    await expect(input).toHaveCSS('border-radius', '8px');
    for (const card of await stack.locator('.import-card').all()) await expect(card).toHaveCSS('border-radius', '12px');
    await expect(stack.locator('.icon-btn')).toHaveCSS('width', '32px');
    const disabled = stack.getByRole('button', { name: 'Import exam', exact: true });
    await expect(disabled).toHaveCSS('opacity', '1');
    await expect(disabled).toHaveCSS('background-color', await paint(stack.getByRole('note'), 'backgroundColor'));
    for (const control of await stack.locator('button').all()) await expect(control).toHaveCSS('height', '32px');
    await expect(stack.getByRole('checkbox')).toHaveCSS('width', '16px');
    await expect(stack).toHaveCSS('gap', '12px');
    for (const header of await headers.all()) {
      await expect(header).toHaveCSS('padding', '8px 16px');
      await expect(header.locator('h3')).toHaveCSS('font-size', '16px');
    }
    const sizes = await headers.evaluateAll(elements => elements.map(el => el.getBoundingClientRect().height));
    if (width > 480) expect(sizes[0]).toBe(sizes[1]);
    else {
      const badge = await stack.locator('.validated').boundingBox();
      const title = await stack.getByRole('heading', { name: 'Exam file' }).boundingBox();
      expect(badge.y).toBeGreaterThan(title.y + title.height);
    }
    const chip = stack.locator('.file-chip');
    await expect(chip).toHaveCSS('text-overflow', 'ellipsis');
    expect(await chip.getAttribute('title')).toMatch(/calculator-test-.*exam\.md/);
    expect(await chip.evaluate(el => el.scrollWidth > el.clientWidth)).toBe(true);
    expect(await stack.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
    const bounds = await stack.boundingBox();
    expect(bounds.width).toBeLessThanOrEqual(560);
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(width);
    const row = stack.locator('.row').first();
    const label = await row.locator('dt').boundingBox(), value = await row.locator('dd').boundingBox();
    if (width < 480) expect(value.y).toBeGreaterThan(label.y);
    else expect(value.x - label.x).toBe(116);
    await stack.getByRole('button', { name: 'Preview the exam', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Prueba manual de calculadora', exact: true });
    await expect(dialog.locator('.exam-preview')).toBeVisible();
    const dialogBounds = await dialog.boundingBox();
    expect(dialogBounds.x).toBeGreaterThanOrEqual(0);
    expect(dialogBounds.x + dialogBounds.width).toBeLessThanOrEqual(width);
    expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
    await dialog.getByRole('button', { name: 'Close', exact: true }).click();
    await stack.getByRole('button', { name: 'Cancel', exact: true }).click();
    expect(await calls(page)).toMatchObject({ importExam: 0, discardImport: 1 });
  });
}

test('the import dialog previews an exam without a calculator without fetching or discarding it', async ({ page }) => {
  const stack = await openImport(page, { withCalculator: false });
  await stack.getByRole('button', { name: 'Preview the exam', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Prueba manual de calculadora', exact: true });
  await expect(dialog).toContainText('This exam has no calculator.');
  await expect(dialog).not.toContainText('after importing it');
  await expect(dialog.locator('.exam-preview')).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Open the calculator' })).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  expect(await page.evaluate(() => window.ProctorHarness.nativeCalls)).toEqual([]);
  expect(await calls(page)).toMatchObject({ importExam: 0, discardImport: 0 });
  await stack.getByRole('button', { name: 'Cancel', exact: true }).click();
  expect(await calls(page)).toMatchObject({ importExam: 0, discardImport: 1 });
});

test('a scheduled login window is not misrepresented as unrestricted', async ({ page }) => {
  const stack = await openImport(page, { scheduled: true });
  const logins = stack.getByRole('definition').last();
  await expect(logins).not.toHaveText('Allowed any time');
  await expect(logins).toContainText('→');
});

test('touch controls share a 36px height', async ({ browser }) => {
  const context = await browser.newContext({ hasTouch: true, viewport: { width: 400, height: 1000 } });
  const page = await context.newPage();
  try {
    const stack = await openImport(page);
    for (const control of await stack.locator('button, input:not([type="checkbox"])').all()) await expect(control).toHaveCSS('height', '36px');
  } finally { await context.close(); }
});
