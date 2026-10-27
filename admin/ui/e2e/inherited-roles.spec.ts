import { expect, test } from '@playwright/test';
import { adminLogin, adminPassword, manage, signIn, tag } from './helpers';

test("a user's page lists the roles they get through their groups", async ({ page }) => {
  const t = tag();
  const user = await manage('POST', '/users', { login: `member-${t}`, name: `Member ${t}`, password: `Pw-${t}-correct-horse-1` });
  const group = await manage('POST', '/groups', { name: `Writers ${t}` });
  const role = (await manage('GET', '/roles?limit=200')).items.find((r: any) => r.name === 'editor');
  await manage('POST', '/assignments', { roleId: role.id, principalKind: 'group', principalId: group.id, limitations: {} });
  await manage('PUT', `/groups/${group.id}/members/${user.id}`);

  await signIn(page, adminLogin, adminPassword);
  await page.goto(`/admin/settings/users/${user.id}`);
  await expect(page.getByText('No roles assigned directly.')).toBeVisible();
  const inherited = page.getByRole('table', { name: 'Roles through groups' });
  await expect(inherited.getByRole('link', { name: 'editor' })).toBeVisible();
  await expect(inherited.getByRole('link', { name: `Writers ${t}` })).toHaveAttribute('href', `/admin/settings/groups/${group.id}`);
  await page.screenshot({ path: process.env.SHOT ?? 'test-results/inherited-roles.png', fullPage: true });
});
