import { expect } from '@playwright/test';

export const nav = page => page.getByRole('navigation', { name: 'Main navigation' });
export const connectionDialog = page => page.getByRole('dialog', { name: 'Server connection' });
export const goTo = (page, label) => nav(page).getByRole('button', { name: label, exact: true }).click();
// A class card on the class list.
export const card = (page, name) => page.getByRole('button', { name: new RegExp(name) });
// The bar at the top of the dashboard that says what students are being admitted to.
export const admitting = page => page.getByRole('region', { name: 'Now admitting' });
// What the bar says while an exam is published: the exam, its class and how many of its students are working.
export const admittingLine = (klass, exam, working) => `Admitting now ${exam} · ${klass} ${working} working`;
// The exams of the class that is open, and its tabs.
export const examsPanel = page => page.getByRole('region', { name: 'Exams', exact: true });
export const tab = (page, name) => page.getByRole('tab', { name });
// One exam of the class that is open: its card, an article named after the exam, inside the panel `exams`.
export const examCard = (exams, title) => exams.getByRole('article', { name: title, exact: true });

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

// Ticks what the publish dialog asks for: the override when students are still working in the exam it replaces, then the plain
// confirmation. The override only exists in that case. A tick already made is left alone, since clicking a label toggles it.
export async function tickPublish(dialog) {
  for (const label of [dialog.locator('label.override'), dialog.locator('label.confirm:not(.override)')]) {
    if (await label.count() && !await label.locator('input').isChecked()) await label.click();
  }
}

// Ticks everything the close-admission dialog asks for: the override when students are still working, then the plain confirmation.
// It draws its ticks like the publish dialog, so it is the same.
export const tickClose = tickPublish;
