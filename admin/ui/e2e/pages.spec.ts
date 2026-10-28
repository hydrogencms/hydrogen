import { expect, test } from '@playwright/test';
import { adminLogin, adminPassword, seedPage, seedRoots, signIn, tag } from './helpers';

const t = tag();

test('selecting a page in a narrow pane never slides the details panel in or out', async ({ page }) => {
  const roots = await seedRoots();
  const a = await seedPage('careers', `Pane A ${t}`, roots.careers.node);
  const b = await seedPage('careers', `Pane B ${t}`, roots.careers.node);
  await signIn(page, adminLogin, adminPassword);
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto(`/admin/pages/careers?node=${a.node}`);
  await expect(page.getByRole('heading', { name: a.title })).toBeVisible();

  await page.evaluate(() => {
    (window as any).__sideTransitions = 0;
    document.addEventListener('transitionrun', (e) => {
      if ((e.target as Element).classList?.contains('h-item__side')) (window as any).__sideTransitions++;
    });
  });
  const tree = page.getByRole('tree', { name: /careers/ });
  await tree.getByRole('treeitem', { name: new RegExp(b.title) }).click();
  await expect(page.getByRole('heading', { name: b.title })).toBeVisible();
  await page.waitForTimeout(500);
  expect(await page.evaluate(() => (window as any).__sideTransitions)).toBe(0);
  await expect(page.locator('.h-item__side')).toBeHidden();
});
