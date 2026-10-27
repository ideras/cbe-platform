import { test, expect } from '@playwright/test';

async function login(page) {
  await page.getByRole('button', { name: 'Test screenshot capture' }).click();
  await expect(page.getByLabel('Screenshot test result')).toContainText('Screenshot capture succeeded');
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123');
  await page.getByLabel('Exam PIN').fill('synthetic PIN');
  await page.getByRole('button', { name: 'Login / resume' }).click();
  await expect(page.getByText('Fixture exam')).toBeVisible();
}
async function token(page, name) {
  return page.evaluate(name => {
    const probe = document.createElement('span'); probe.style.color = `var(--${name})`;
    document.body.append(probe); const color = getComputedStyle(probe).color; probe.remove(); return color;
  }, name);
}

test('CodeMirror search decorations and calculator modes use the shared semantic palette', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('button.primary')).toHaveCount(1);
  await expect(page.getByRole('button', { name: 'Test screenshot capture' })).not.toHaveClass(/primary/);
  await page.evaluate(() => window.__studentHarness.calculatorTools({ calculator: { enabled: true, modes: ['scientific', 'programmer'], angle_mode: 'user' } }));
  await login(page);
  const content = page.getByRole('textbox', { name: 'Answer for q1' });
  await content.click();
  await page.keyboard.press('Control+f');
  await expect(content).toContainText('restored work');
  await page.locator('.cm-search input[name="search"]').pressSequentially('restored');
  const match = page.locator('.cm-searchMatch').first();
  await expect(match).toHaveCSS('background-color', await token(page, 'selected'));
  await expect(match).toHaveCSS('color', await token(page, 'ink'));
  await page.keyboard.press('Escape');
  await expect(page.locator('.editor').first()).toHaveCSS('border-top-color', await token(page, 'ocean'));
  await page.getByRole('button', { name: 'Calculator', exact: true }).click();
  const calculator = page.getByRole('dialog', { name: 'Calculator' });
  const ocean = await token(page, 'ocean');
  await expect(calculator.getByRole('tab', { name: 'Scientific' })).toHaveCSS('background-color', ocean);
  await expect(calculator.getByRole('button', { name: 'Evaluate' })).toHaveCSS('background-color', ocean);
  await expect(calculator.getByRole('button', { name: 'sin', exact: true })).toHaveCSS('background-color', await token(page, 'sea'));
  await calculator.getByRole('tab', { name: 'Programmer' }).click();
  await expect(calculator.locator('.seg button[aria-pressed="true"]').first()).toHaveCSS('background-color', await token(page, 'ink'));
  await expect(calculator.locator('.key.sym').first()).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
});

test('forward-only steps have one primary action, switching to final submit on the last exercise', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.__studentHarness.navigation('forward_only'));
  await login(page);
  const submit = page.getByRole('button', { name: 'Submit current answers — final' });
  for (const next of [2, 3]) {
    const advance = page.getByRole('button', { name: `Continue to exercise ${next} →` });
    await expect(advance).toHaveClass(/primary/);
    await expect(submit).not.toHaveClass(/primary/);
    await expect(page.locator('main button.primary')).toHaveCount(1);
    await advance.click();
    await page.getByRole('dialog').getByRole('button', { name: `Continue to exercise ${next}`, exact: true }).click();
    await expect(page.getByText(`Exercise ${next} of 3.`, { exact: false })).toBeVisible();
  }
  await expect(submit).toHaveClass(/primary/);
  await expect(page.locator('main button.primary')).toHaveCount(1);
});
