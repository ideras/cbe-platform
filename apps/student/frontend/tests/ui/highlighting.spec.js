import { test, expect } from '@playwright/test';

test('answer-only C, C++, Python and MIPS highlighting has no completions; unknown languages are plain', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.__studentHarness.exam({ navigation: 'free', content: { exercises: [{ id: '1', title: 'Languages',
    body: '<pre><code>int readonly = 1;</code></pre>' + ['c', 'cpp', 'py', 'mips', 'java'].map(id => `<div class="slot" data-slot="${id}"></div>`).join(''),
    slots: [
      { id: 'c', kind: 'code', language: 'c', starter: 'int value = 42; return value;' },
      { id: 'cpp', kind: 'code', language: 'c++', starter: 'class Example {};' },
      { id: 'py', kind: 'code', language: 'python', starter: 'def example():\n    return 42' },
      { id: 'mips', kind: 'code', language: 'mips', starter: 'addi $t0, $sp, 42 # comment' },
      { id: 'java', kind: 'code', language: 'java', starter: 'class Plain {}' },
    ],
  }] } }));
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123');
  await page.getByLabel('Exam PIN').fill('PIN');
  await page.getByRole('button', { name: 'Login / resume' }).click();
  for (const [language, id, keyword] of [['c', 'c', 'return'], ['c++', 'cpp', 'class'], ['python', 'py', 'def'], ['mips', 'mips', 'addi']]) {
    const editor = page.getByRole('textbox', { name: `${language} code answer for ${id}`, exact: true });
    await expect(editor.locator('span').filter({ hasText: new RegExp(`^${keyword}$`) }).first()).toHaveCSS('font-weight', '700');
    await editor.press('Control+End');
    await editor.press('End');
    await editor.pressSequentially(' pri');
    await page.waitForTimeout(200);
    await expect(page.locator('.cm-tooltip-autocomplete')).toHaveCount(0);
  }
  await expect(page.getByRole('textbox', { name: 'java code answer for java' }).locator('span')).toHaveCount(0);
  await expect(page.locator('pre code span')).toHaveCount(0);
});
