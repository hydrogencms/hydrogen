import { expect, test } from '@playwright/test';

const login = process.env.ADMIN_LOGIN ?? 'admin';
const password = process.env.ADMIN_PASSWORD ?? 'e2e-admin-password-1';

test('login, shell and dashboard', async ({ page }) => {
  const errors: string[] = [];
  page.on('console', (m) => m.type() === 'error' && !m.text().includes('401') && errors.push(m.text()));
  page.on('pageerror', (e) => errors.push(String(e)));

  await page.goto('/admin/collections/article');
  await expect(page).toHaveURL(/\/admin\/login\?next=%2Fcollections%2Farticle/);
  await page.getByLabel('Login').fill(login);
  await page.getByLabel('Password').fill('wrong');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('alert')).toContainText('not right');

  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/admin\/collections\/article$/);
  await expect(page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Article' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Article', level: 1 })).toBeVisible();

  await page.getByRole('link', { name: 'Dashboard' }).click();
  await expect(page.getByRole('heading', { name: /Welcome/ })).toBeVisible();
  await expect(page.getByText('Recent activity')).toBeVisible();
  await expect(page.getByRole('link', { name: 'Users' })).toBeVisible();

  await page.getByLabel('Search content').fill('hello');
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(/\/admin\/search\?q=hello/);

  await page.getByRole('button', { name: 'Account menu' }).click();
  await page.getByRole('menuitem', { name: 'Sign out' }).click();
  await expect(page).toHaveURL(/\/admin\/login/);
  expect(errors).toEqual([]);
});
