import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { adminLogin, adminPassword, manage, seedPage, seedRoots, signIn, tag, watchErrors } from './helpers';

// No serious or critical axe violations on the main screens (WCAG 2.x A/AA and best practices).
async function violations(page: Page) {
  const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'best-practice']).analyze();
  expect(result.passes.length, 'axe ran').toBeGreaterThan(10);
  return result.violations
    .filter((v) => v.impact === 'serious' || v.impact === 'critical')
    .map((v) => `${v.id} (${v.impact}): ${v.help}\n${v.nodes.slice(0, 3).map((n) => '    ' + n.target.join(' ')).join('\n')}`);
}

test.describe('accessibility', () => {
  let item = '';
  let node = '';
  let role = '';

  test.beforeAll(async () => {
    const roots = await seedRoots();
    node = roots.careers.node;
    item = (await seedPage('careers', `Axe page ${tag()}`, roots.careers.node, { kind: 'team' })).item;
    role = (await manage('GET', '/roles?limit=100')).items.find((r: any) => r.name === 'editor').id;
  });

  test('login', async ({ page }) => {
    const errors = watchErrors(page);
    await page.goto('/admin/login');
    await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible();
    expect(await violations(page)).toEqual([]);
    expect(errors).toEqual([]);
  });

  const screens: [string, () => string, (page: Page) => Promise<void>][] = [
    ['collection list', () => '/admin/collections/page', (p) => expect(p.getByRole('table', { name: 'Page items' }).getByRole('link').first()).toBeVisible()],
    [
      'item editor',
      () => `/admin/items/${item}`,
      async (p) => {
        await expect(p.getByRole('form', { name: /fields/ })).toBeVisible();
        await expect(p.getByRole('tabpanel', { name: 'Appears in' })).toBeVisible();
      },
    ],
    [
      'pages tree with the embedded editor',
      () => `/admin/pages/careers?node=${node}`,
      async (p) => {
        await expect(p.getByRole('tree', { name: /careers/ }).getByRole('treeitem').first()).toBeVisible();
        await expect(p.getByRole('region', { name: 'Selected page' }).getByRole('form')).toBeVisible();
      },
    ],
    ['roles editor', () => `/admin/settings/roles/${role}`, (p) => expect(p.getByRole('heading', { level: 1, name: 'editor' })).toBeVisible()],
  ];
  for (const [name, url, ready] of screens) {
    test(name, async ({ page }) => {
      await signIn(page, adminLogin, adminPassword);
      await page.goto(url());
      await ready(page);
      expect(await violations(page)).toEqual([]);
    });
  }
});
