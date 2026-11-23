import { test, expect } from '@playwright/test';

const files = 'Download all student files';
const reports = 'Download all evaluation reports';
const group = page => page.getByRole('group', { name: 'Workspace downloads' });
const download = (page, title) => group(page).getByRole('button', { name: `${title}…`, exact: true });
const dialog = (page, title) => page.getByRole('dialog', { name: title, exact: true });
const calls = page => page.evaluate(() => (window.ProctorHarness.calls.workspace ?? []).filter(item => item.call === 'DownloadWorkspaceBatch'));
const result = {
  cancelled: false, folder: '/example/output/LongFolderNameWithoutWhitespaceForTheLayoutCheck', written: 1, skipped: 1, failed: 1,
  issues: [
    { student: { student_name: 'Grace Demo', account_number: '0002' }, status: 'skipped', reason: 'No evaluation report is available.' },
    { student: { student_name: 'Alan Demo', account_number: '0003' }, status: 'failed', reason: 'The server connection changed.' },
  ],
};

test.describe('narrow window', () => {
    test.use({ viewport: { width: 390, height: 620 } });

    test('workspace actions ignore roster filters, use one batch each, and support keyboard at 390px', async ({ page }) => {
      await page.goto('/#/monitoring');
      await expect(page.getByRole('table', { name: /All sessions/ })).toBeVisible();
      await expect(group(page)).toHaveCount(0);
      const scope = await page.evaluate(async () => {
        window.ProctorHarness.workspace({ students: [] });
        const view = await import('/tests/fake-api.js').then(api => api.Snapshot());
        return { examId: view.exam_id, server: view.server };
      });
      await expect(group(page)).toBeVisible();
      const filter = page.getByRole('group', { name: 'Filter sessions' }).getByRole('button', { name: /^Submitted / });
      await filter.click();
      const trigger = download(page, files);
      await trigger.press('Enter');
      const filesDialog = dialog(page, files);
      const chooseFiles = filesDialog.getByRole('button', { name: 'Choose folder…' });
      await expect(filesDialog).toContainText('Entire exam');
      await expect(filesDialog.getByRole('checkbox')).toHaveCount(0);
      await expect(chooseFiles).toBeFocused();
      expect(await calls(page)).toEqual([]);
      await page.keyboard.press('Escape');
      await expect(filesDialog).toHaveCount(0);
      await expect(trigger).toBeFocused();

      await trigger.press('Enter');
      await chooseFiles.press('Space'); // the fake picker cancels
      await expect(filesDialog).toHaveCount(0);
      await expect(trigger).toBeFocused();
      expect(await calls(page)).toEqual([{ call: 'DownloadWorkspaceBatch', ...scope, kind: 'files', overwrite: false }]);
      await expect(filter).toHaveAttribute('aria-pressed', 'true');

      await page.evaluate(value => window.ProctorHarness.workspaceBatch(value, true), result);
      await download(page, reports).press('Enter');
      const reportsDialog = dialog(page, reports);
      await expect(reportsDialog).toContainText('based on the TODO items in Student Review → Grading');
      await expect(reportsDialog).toContainText('The latest submission must have a completed grading run');
      await expect(reportsDialog).toContainText('feedback shown when a TODO is expanded');
      const overwrite = reportsDialog.getByRole('checkbox', { name: 'Overwrite existing files' });
      await expect(overwrite).not.toBeChecked();
      await overwrite.press('Space');
      await expect(overwrite).toBeChecked();
      await expect(reportsDialog).toContainText('Matching reports will be replaced');
      const chooseReports = reportsDialog.getByRole('button', { name: 'Choose folder…' });
      await chooseReports.press('Enter');
      await expect(chooseReports).toBeDisabled();
      await expect(reportsDialog.getByRole('status')).toHaveText('1 of 3 students checked');
      await page.keyboard.press('Enter');
      expect(await calls(page)).toEqual([
        { call: 'DownloadWorkspaceBatch', ...scope, kind: 'files', overwrite: false },
        { call: 'DownloadWorkspaceBatch', ...scope, kind: 'reports', overwrite: true },
      ]);
      await page.evaluate(() => window.ProctorHarness.releaseWorkspaceBatch());
      await expect(reportsDialog.getByRole('status')).toContainText('1 written · 1 skipped · 1 failed');
      await expect(reportsDialog.getByRole('listitem')).toHaveText([
        'Grace Demo (0002)Skipped: No evaluation report is available.',
        'Alan Demo (0003)Failed: The server connection changed.',
      ]);
      expect(await reportsDialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
      const done = reportsDialog.getByRole('button', { name: 'Done' });
      await expect(done).toBeFocused();
      await page.keyboard.press('Enter');
      await expect(reportsDialog).toHaveCount(0);
      await expect(download(page, reports)).toBeFocused();
    });
});

test('a server switch does not discard the result from the previous roster', async ({ page }) => {
  await page.goto('/#/monitoring');
  await page.getByRole('table', { name: /All sessions/ }).waitFor();
  await page.evaluate(value => {
    window.ProctorHarness.workspace({ students: [] });
    window.ProctorHarness.workspaceBatch(value, true);
  }, result);
  await download(page, reports).click();
  const modal = dialog(page, reports);
  await modal.getByRole('button', { name: 'Choose folder…' }).click();
  await page.evaluate(() => window.ProctorHarness.disconnected());
  await expect(page.getByText(/Not connected to the server/)).toBeVisible();
  await expect(modal.getByRole('progressbar')).toBeVisible();
  expect(await calls(page)).toMatchObject([{ kind: 'reports', overwrite: false }]);
  await page.evaluate(() => window.ProctorHarness.releaseWorkspaceBatch());
  await expect(modal.getByRole('status')).toContainText('1 written · 1 skipped · 1 failed');
});
