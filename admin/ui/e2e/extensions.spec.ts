import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { add, card, expand } from './blocks-helpers';
import { randomPNG } from './media-helpers';
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
    // the list action test selects two articles
    await manage('POST', '/collections/article/items', { locale: 'en', fields: { title: `Extensions second ${t}`, summary: 'Another.' } });
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

  async function serious(page: Page, skip: string[] = []) {
    const result = await skip.reduce((b, sel) => b.exclude(sel), new AxeBuilder({ page })).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'best-practice']).analyze();
    expect(result.passes.length, 'axe ran').toBeGreaterThan(10);
    return result.violations
      .filter((v) => v.impact === 'serious' || v.impact === 'critical')
      .map((v) => `${v.id} (${v.impact}): ${v.help}\n${v.nodes.slice(0, 3).map((n) => '    ' + n.target.join(' ')).join('\n')}`);
  }

  test('block preview and block prop widget: palette tile, card summary, colour round-trips', async ({ page }) => {
    const errors = watchErrors(page);
    const doc = await manage('POST', '/collections/page/items', { locale: 'en', fields: { title: `Blocks page ${t}` } });
    await signIn(page, adminLogin, adminPassword);
    await page.goto(`/admin/items/${doc.id}`);
    const trigger = page.locator('[data-blocks="blocks"] > .h-blocklist > .h-blocklist__foot').getByRole('button', { name: 'Add block' });
    await trigger.click();
    // palette: the hero tile renders the preview element with the type's defaults, the quote tile stays built in
    const tile = page.getByRole('option', { name: /^Hero/ });
    await expect(tile.locator('[data-preview="x-hero-preview"]')).toBeVisible();
    await expect(page.getByRole('option', { name: /^Quote/ }).locator('[data-preview]')).toHaveCount(0);
    expect(await serious(page)).toEqual([]);
    await tile.click();

    const hero = card(page, 'hero');
    await hero.getByLabel(/^Heading/).fill('Hello preview');
    await hero.getByLabel('Brand colour (hex)').fill('#336699');
    await expect(hero.getByLabel('Brand colour', { exact: true })).toHaveValue('#336699');
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByText('Saved', { exact: true }).first()).toBeVisible();
    const saved = (await draftOf(doc.id)).fields.blocks;
    expect(JSON.stringify(saved)).toContain('#336699');

    await page.reload();
    const again = card(page, 'hero');
    // collapsed card summary: heading text and the colour swatch
    const summary = again.locator('[data-preview="x-hero-preview"]').first();
    await expect(summary.getByTestId('hero-heading')).toHaveText('Hello preview');
    const bg = await summary.getByTestId('hero-swatch').evaluate((e) => getComputedStyle(e).backgroundColor);
    expect(bg).toBe('rgb(51, 102, 153)');
    await expand(again);
    await expect(again.getByLabel('Brand colour (hex)')).toHaveValue('#336699');
    expect(await serious(page)).toEqual([]);
    expect(errors).toEqual([]);
  });

  test('list action: the toolbar button counts the selected items', async ({ page }) => {
    const errors = watchErrors(page);
    await signIn(page, adminLogin, adminPassword);
    await page.goto('/admin/collections/article');
    const action = page.locator('[data-extension="x-export-csv"]');
    await expect(action.first().getByTestId('export-count')).toHaveText('0');
    const rows = page.locator('tbody').getByRole('row').filter({ has: page.getByRole('checkbox') });
    await rows.nth(0).getByRole('checkbox').check();
    await rows.nth(1).getByRole('checkbox').check();
    await expect(action.getByTestId('export-count').last()).toHaveText('2');
    await action.last().getByRole('button').click();
    await expect(page.getByText('Exported 2 article items')).toBeVisible();
    expect(await serious(page)).toEqual([]);
    expect(errors).toEqual([]);
    // not offered on a collection the action does not list
    await page.goto('/admin/collections/person');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    await expect(page.locator('[data-extension="x-export-csv"]')).toHaveCount(0);
  });

  test('assets: the slot renders and the alt check panel shows on an image', async ({ page }) => {
    const errors = watchErrors(page);
    const name = `ext-${t}.png`;
    await signIn(page, adminLogin, adminPassword);
    await page.goto('/admin/assets');
    await expect(page.locator('[data-slot="assets.before"]')).toContainText('Media guidelines apply (assets.before)');
    await page.locator('input[type=file]').first().setInputFiles({ name, mimeType: 'image/png', buffer: randomPNG() });
    await page.getByText(name).first().click();
    const panel = page.locator('[data-extension="x-alt-check"]');
    await expect(panel.getByTestId('alt-state')).toHaveText('Alt text is missing.');
    expect(await serious(page)).toEqual([]);
    expect(errors).toEqual([]);
  });

  test('palette: lists the Import view', async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
    await page.keyboard.press('ControlOrMeta+k');
    await expect(page.getByRole('combobox')).toBeVisible();
    await expect(page.getByRole('listbox').getByRole('option', { name: /Import/ })).toBeVisible();
    await page.getByRole('listbox').getByRole('option', { name: /Import/ }).click();
    await expect(page).toHaveURL(/\/admin\/tools\/import$/);
    expect(await serious(page)).toEqual([]);
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
