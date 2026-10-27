import { expect, test } from '@playwright/test';
import { adminLogin, adminPassword, manage, signIn, tag } from './helpers';

test('audit details open in a dialog that says what happened in words', async ({ page }) => {
  const t = tag();
  const user = await manage('POST', '/users', { login: `joiner-${t}`, name: `Joiner ${t}`, password: `Pw-${t}-correct-horse-1` });
  const group = await manage('POST', '/groups', { name: `Crew ${t}` });
  await manage('PUT', `/groups/${group.id}/members/${user.id}`);

  await signIn(page, adminLogin, adminPassword);
  await page.goto(`/admin/settings/audit?action=group.member_add&subject=${group.id}`);
  const table = page.getByRole('table');
  const row = table.getByRole('row').nth(1);
  const before = await table.boundingBox();
  await row.getByRole('button', { name: 'Details' }).click();
  const dlg = page.getByRole('dialog', { name: 'Event details' });
  await expect(dlg.getByText(new RegExp(`^Added (joiner-|Joiner )${t} to the group Crew ${t}$`))).toBeVisible();
  await expect(dlg.getByText(user.id)).toBeHidden();
  expect(await table.boundingBox()).toEqual(before);
  await dlg.getByText('Technical details').click();
  await expect(dlg.getByText(user.id)).toBeVisible();
});
