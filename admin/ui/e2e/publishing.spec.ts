import { expect, test } from '@playwright/test';
import { adminLogin, adminPassword, manage, seedPage, seedRoots, signIn, tag, watchErrors } from './helpers';
import { axeViolations, deliveredStatus, draftArticle, expectDelivered, publishedArticle } from './publishing-helpers';

// H8a: unpublish, trash, restore, purge and the block-by-reference refusal. Setup and assertions on delivery go
// through the management and delivery APIs; the actions under test go through the admin.
test.describe('unpublish and trash', () => {
  test.beforeEach(async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
  });

  test('unpublish stops delivery, keeps the draft and can be published again', async ({ page }) => {
    const errors = watchErrors(page);
    const a = await publishedArticle('Unpublish me');
    await expectDelivered(a.id, 200);

    await page.goto(`/admin/items/${a.id}`);
    await page.getByRole('button', { name: 'More actions' }).click();
    await page.getByRole('menuitem', { name: 'Unpublish', exact: true }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Unpublish', exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(page.locator('.h-badge', { hasText: /^Unpublished$/ }).first()).toBeVisible();

    await expectDelivered(a.id, 404);
    await manage('GET', `/items/${a.id}`, undefined, [404]);
    const item = await manage('GET', `/items/${a.id}?draft=true&locale=en`);
    expect(item.status).toBe('unpublished');
    expect((await manage('GET', `/items/${a.id}?draft=true&locale=en`)).fields.title).toBe(a.title);

    await manage('POST', `/items/${a.id}/publish`, {});
    await expectDelivered(a.id, 200);
    expect(errors).toEqual([]);
  });

  test('trash hides the item, restore brings it back unpublished', async ({ page }) => {
    const a = await publishedArticle('Trash me');

    await page.goto(`/admin/items/${a.id}`);
    await page.getByRole('button', { name: 'More actions' }).click();
    await page.getByRole('menuitem', { name: 'Move to trash' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Move to trash' }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);

    // Gone from the list, reads and delivery; present in the trash.
    await expectDelivered(a.id, 404);
    await manage('GET', `/items/${a.id}`, undefined, [404]);
    const list = await manage('GET', `/collections/article/items?q=${encodeURIComponent(a.title)}`);
    expect(list.items.map((i: any) => i.id)).not.toContain(a.id);
    const trash = await manage('GET', '/trash?type=article&limit=100');
    const row = trash.items.find((i: any) => i.id === a.id);
    expect(row?.deletedAt).toBeTruthy();

    await page.goto('/admin/collections/article');
    await expect(page.getByRole('table', { name: 'Article items' }).getByText(a.title)).toHaveCount(0);
    await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Trash' }).click();
    await expect(page).toHaveURL(/\/admin\/trash/);
    const tr = page.getByRole('row').filter({ hasText: a.title });
    await expect(tr).toBeVisible();
    await tr.getByRole('button', { name: 'Restore' }).click();
    await expect(tr).toHaveCount(0);

    // Back as an unpublished item with its draft.
    const back = await manage('GET', `/items/${a.id}?draft=true&locale=en`);
    expect(back.status).toBe('unpublished');
    expect((await manage('GET', `/items/${a.id}?draft=true&locale=en`)).fields.title).toBe(a.title);
    expect(await deliveredStatus(a.id)).toBe(404);
    await page.goto(`/admin/items/${a.id}`);
    await expect(page.locator('.h-badge', { hasText: /^Unpublished$/ }).first()).toBeVisible();
  });

  test('delete permanently removes a trashed item for good', async ({ page }) => {
    const a = await draftArticle('Purge me');
    await manage('DELETE', `/items/${a.id}`);

    await page.goto('/admin/trash');
    const tr = page.getByRole('row').filter({ hasText: a.title });
    await tr.getByRole('button', { name: 'Delete permanently' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Delete permanently' }).click();
    await expect(tr).toHaveCount(0);

    await manage('GET', `/items/${a.id}?trashed=true&draft=true`, undefined, [404]);
    const trash = await manage('GET', '/trash?type=article&limit=100');
    expect(trash.items.map((i: any) => i.id)).not.toContain(a.id);
  });

  test('purging an item that is not in the trash is refused', async () => {
    const a = await draftArticle('Not trashed');
    await manage('DELETE', `/items/${a.id}?permanent=true`, undefined, [409]);
    expect((await manage('GET', `/items/${a.id}?draft=true`)).id).toBe(a.id);
  });

  test('a published referrer with on_delete: block refuses the trash and names the referrer', async ({ page }) => {
    const roots = await seedRoots();
    const t = tag();
    const target = await seedPage('careers', `Target ${t}`, roots.careers.node);
    const referrer = await seedPage('careers', `Referrer ${t}`, roots.careers.node, { related: target.item });

    await page.goto(`/admin/items/${target.item}`);
    await page.getByRole('button', { name: 'More actions' }).click();
    await page.getByRole('menuitem', { name: 'Move to trash' }).click();
    const dlg = page.getByRole('dialog');
    await dlg.getByRole('button', { name: 'Move to trash' }).click();
    await expect(dlg.getByRole('alert')).toContainText('Referenced by 1 items');
    await expect(dlg.getByRole('link', { name: referrer.title })).toBeVisible();
    await dlg.getByRole('button', { name: 'Cancel' }).click();

    expect((await manage('GET', `/items/${target.item}?draft=true`)).id).toBe(target.item);
    const trash = await manage('GET', '/trash?limit=100');
    expect(trash.items.map((i: any) => i.id)).not.toContain(target.item);
  });

  test('the Trash screen is accessible', async ({ page }) => {
    const a = await draftArticle('Axe trash');
    await manage('DELETE', `/items/${a.id}`);
    await page.goto('/admin/trash');
    await expect(page.getByRole('row').filter({ hasText: a.title })).toBeVisible();
    expect(await axeViolations(page)).toEqual([]);
  });
});
