import { expect, test } from '@playwright/test';
import { adminLogin, adminPassword, openSettings, signIn, watchErrors } from './helpers';
import { axeViolations } from './publishing-helpers';

// H10a: the read-only System page (GET /manage/v1/system) on the multisite e2e setup.
test('the System page lists the core version and the registered modules', async ({ page }) => {
  const errors = watchErrors(page);
  await signIn(page, adminLogin, adminPassword);
  await openSettings(page);
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'System' }).click();
  await expect(page).toHaveURL(/\/admin\/settings\/system/);

  await expect(page.getByTestId('system-core')).not.toBeEmpty();
  await expect(page.getByTestId('system-go')).toContainText(/go\d/);
  await expect(page.getByTestId('system-database')).toHaveText(/sqlite|postgres/);

  const table = page.getByRole('table', { name: 'Modules' });
  await expect(table).toBeVisible();
  const row = table.getByRole('row').filter({ has: page.getByRole('cell', { name: 'pages', exact: true }) });
  await expect(row).toHaveCount(1);
  await expect(table.getByRole('cell', { name: 'overlays', exact: true })).toBeVisible();

  expect(await axeViolations(page)).toEqual([]);
  expect(errors).toEqual([]);
});
