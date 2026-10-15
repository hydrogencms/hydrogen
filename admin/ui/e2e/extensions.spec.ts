import { expect, test, type Page } from '@playwright/test';
import { adminLogin, adminPassword, draftOf, manage, signIn, tag, watchErrors } from './helpers';

// Runs against examples/content (E2E_PROJECT=content in scripts/admin-e2e.sh): every extension kind of its admin/.
test.describe.serial('admin extensions (examples/content)', () => {
  const t = tag();
  const viewer = { login: `viewer-${t}`, password: `Pw-${t}-correct-horse-1` };
  const nav = (page: Page) => page.getByRole('navigation', { name: 'Main' });
  let article = '';
  let other = '';

  test.beforeAll(async () => {
    const user = await manage('POST', '/users', { login: viewer.login, name: 'E2E Viewer', password: viewer.password });
    const roles = await manage('GET', '/roles?limit=200');
    const role = roles.items.find((r: { name: string }) => r.name === 'viewer');
    await manage('POST', '/assignments', { roleId: role.id, principalKind: 'user', principalId: user.id, limitations: {} });
    const a = await manage('POST', '/collections/article/items', {
      locale: 'en', fields: { title: `Extensions article ${t}`, summary: 'A short summary.', color: '#ff8800' },
    });
    await manage('POST', `/items/${a.id}/publish`, {});
    article = a.id;
    const p = await manage('POST', '/collections/person/items', { locale: 'en', fields: { firstName: 'Ext', lastName: t } });
    other = p.id;
  });

  test('theme: name, logo, accent and a string override', async ({ page }) => {
    const errors = watchErrors(page);
    await signIn(page, adminLogin, adminPassword);
    await expect(page).toHaveTitle(/Studio CMS/);
    await expect(page.locator('.h-nav__logo')).toBeVisible();
    await expect(page.locator('.h-nav__logo')).toHaveAttribute('src', /\/admin\/x\/logo\.svg$/);
    await expect(page.locator('.h-nav__brand')).toContainText('Studio CMS');
    const accent = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--h-accent').trim());
    expect(accent.toLowerCase()).toBe('#7c3aed');
    await expect(page.getByRole('heading', { name: /^Studio overview for/, level: 1 })).toBeVisible();
    expect(errors).toEqual([]);
  });

  test('widget: a colour set in the editor saves and round-trips', async ({ page }) => {
    const errors = watchErrors(page);
    await signIn(page, adminLogin, adminPassword);
    await page.goto(`/admin/items/${article}`);
    const hex = page.getByLabel('Brand colour (hex)');
    await expect(hex).toHaveValue('#ff8800');
    await hex.fill('#112233');
    await expect(page.getByLabel('Brand colour', { exact: true })).toHaveValue('#112233');
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByText('Saved', { exact: true }).first()).toBeVisible();
    expect((await draftOf(article)).fields.color).toBe('#112233');
    await page.reload();
    await expect(page.getByLabel('Brand colour (hex)')).toHaveValue('#112233');
    expect(errors).toEqual([]);
  });

  test('cell: the list shows the swatch', async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
    await page.goto('/admin/collections/article');
    const row = page.getByRole('row', { name: new RegExp(`Extensions article ${t}`) });
    const sw = row.locator('[data-extension="x-color-swatch"]');
    await expect(sw).toContainText('#112233');
    await expect(sw.locator('.chip')).toBeVisible();
    const bg = await sw.locator('.chip').evaluate((e) => getComputedStyle(e).backgroundColor);
    expect(bg).toBe('rgb(17, 34, 51)');
    // sized by the element's own <style> block in its shadow root (needs the admin CSP to allow it)
    expect(await sw.locator('.chip').evaluate((e) => getComputedStyle(e).width)).toBe('16px');
  });

  test('panel: SEO on article, not on person; refresh; published state', async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
    await page.goto(`/admin/items/${article}`);
    await page.getByRole('tab', { name: 'SEO' }).click();
    const panel = page.locator('[data-extension="x-seo-panel"]');
    await expect(panel.getByTestId('seo-score')).toHaveText('50');
    await expect(panel.getByTestId('seo-state')).toHaveText('draft differs');
    await panel.getByRole('button', { name: 'Refresh' }).click();
    await expect(panel.getByTestId('seo-state')).toHaveText('draft differs');
    await page.goto(`/admin/items/${other}`);
    await expect(page.getByRole('tab').first()).toBeVisible();
    await expect(page.getByRole('tab', { name: 'SEO' })).toHaveCount(0);
  });

  test('action: visible for the administrator, toasts, absent for a viewer', async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
    await page.goto(`/admin/items/${article}`);
    await page.locator('[data-action="x-translate-action"]').getByRole('button', { name: 'Send to translation' }).click();
    await expect(page.getByText(/Sent .* to translation/)).toBeVisible();
    await page.context().clearCookies();
    await signIn(page, viewer.login, viewer.password);
    await page.goto(`/admin/items/${article}`);
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    await expect(page.locator('[data-action="x-translate-action"]')).toHaveCount(0);
  });

  test('slot: the banner shows on the editor', async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
    await page.goto(`/admin/items/${article}`);
    await expect(page.locator('[data-slot="editor.before"]')).toContainText(/Editing article as /);
  });

  test('dashboard: the recent widget lists audit entries that link to items', async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
    await page.goto('/admin/');
    const w = page.locator('[data-dashboard-widget="x-recent-widget"]');
    await expect(w.locator('li')).toHaveCount(5);
    await w.locator('a').first().click();
    await expect(page).toHaveURL(/\/admin\/items\/[0-9a-f-]{36}/);
  });

  test('view: Tools menu for the administrator imports an article; hidden for a viewer', async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
    await expect(nav(page).getByText('Tools', { exact: true })).toBeVisible();
    await nav(page).getByRole('link', { name: 'Import' }).click();
    await expect(page).toHaveURL(/\/admin\/tools\/import$/);
    const title = `Imported ${t}`;
    await page.getByLabel('Title').fill(title);
    await page.getByRole('button', { name: 'Import', exact: true }).click();
    await expect(page).toHaveURL(/\/admin\/items\/[0-9a-f-]{36}/);
    const id = page.url().split('/').pop()!;
    expect((await draftOf(id)).fields.title).toBe(title);

    await page.context().clearCookies();
    await signIn(page, viewer.login, viewer.password);
    await expect(nav(page).getByRole('link', { name: 'Import' })).toHaveCount(0);
    await expect(nav(page).getByText('Tools', { exact: true })).toHaveCount(0);
  });

  test('a broken element module toasts once and the built-in editor still works', async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('x-broken', '1'));
    await signIn(page, adminLogin, adminPassword);
    await page.goto(`/admin/items/${article}`);
    await expect(page.getByText(/Admin extension failed to load/)).toHaveCount(1);
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    await expect(page.getByLabel('Title').first()).toBeVisible();
    await page.goto('/admin/collections/article');
    await expect(page.getByRole('row', { name: new RegExp(`Extensions article ${t}`) })).toBeVisible();
    await page.goto(`/admin/items/${article}`);
    await expect(page.getByText(/Admin extension failed to load/)).toHaveCount(1);
    // the good module still loaded: its banner renders next to the one toast
    await expect(page.locator('[data-slot="editor.before"]')).toContainText(/Editing article as /);
  });
});
