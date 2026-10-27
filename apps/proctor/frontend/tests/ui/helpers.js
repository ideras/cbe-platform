import { expect } from '@playwright/test';

export const nav = page => page.getByRole('navigation', { name: 'Main navigation' });
export const connectionDialog = page => page.getByRole('dialog', { name: 'Server connection' });
export const goTo = (page, label) => nav(page).getByRole('button', { name: label, exact: true }).click();
// A class card on the class list.
export const card = (page, name) => page.getByRole('button', { name: new RegExp(name) });
// The bar at the top of the dashboard that says what students are being admitted to.
export const admitting = page => page.getByRole('region', { name: 'Now admitting' });
// The exams of the class that is open, and its tabs.
export const examsPanel = page => page.getByRole('region', { name: 'Exams', exact: true });
export const tab = (page, name) => page.getByRole('tab', { name });

// Opens the application and connects through the Server connection dialog, as a person would. It ends on the class list, or,
// with `openClass`, in that class on its Exams tab.
export async function connect(page, { prepare, openClass = '' } = {}) {
  await page.goto('/');
  if (prepare) await page.evaluate(prepare);
  await nav(page).getByRole('button', { name: 'Server connection' }).click();
  const dialog = connectionDialog(page);
  await dialog.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(nav(page).getByRole('button', { name: 'Classes', exact: true })).toBeEnabled();
  if (openClass) {
    await card(page, openClass).click();
    await expect(page.getByRole('heading', { name: openClass, level: 1 })).toBeVisible();
  }
}

export const calls = page => page.evaluate(() => window.ProctorHarness.calls);
export const harness = (page, run) => page.evaluate(run);
