import { expect, test } from '@playwright/test';
import { adminLogin, adminPassword, manage, seedPage, seedRoots, signIn, tag } from './helpers';

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

test('a publish entry links to the editor, the version it published and the live page', async ({ page }) => {
  const t = tag();
  const roots = await seedRoots();
  const p = await seedPage('careers', `Openings ${t}`, roots.careers.node);
  await manage('PATCH', `/items/${p.item}`, { locale: 'en', fields: { title: `Openings ${t} (fixed)` } });
  await manage('POST', `/items/${p.item}/publish`, {});

  await signIn(page, adminLogin, adminPassword);
  await page.goto(`/admin/settings/audit?action=item.publish&subject=${p.item}`);
  await page.getByRole('table').getByRole('row').nth(1).getByRole('button', { name: 'Details' }).click();
  const dlg = page.getByRole('dialog', { name: 'Event details' });
  await expect(dlg.getByRole('link', { name: 'Open in the editor' })).toHaveAttribute('href', `/admin/items/${p.item}?locale=en`);
  await expect(dlg.getByRole('link', { name: 'compare with version 1' })).toHaveAttribute('href', `/admin/items/${p.item}/compare?a=1&b=2&locale=en`);
  await expect(dlg.getByRole('link', { name: /jobs\.acme\.com\// })).toHaveAttribute('href', new RegExp(`^https://jobs\\.acme\\.com/.*${t}`));
  await dlg.getByRole('link', { name: 'compare with version 1' }).click();
  await expect(page).toHaveURL(new RegExp(`/items/${p.item}/compare`));
  await expect(dlg).toBeHidden();
});
