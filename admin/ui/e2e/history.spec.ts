import { expect, test } from '@playwright/test';
import { adminLogin, adminPassword, deliver, draftOf, manage, signIn, tag, watchErrors } from './helpers';
import { axeViolations, draftArticle, expectDelivered } from './publishing-helpers';
import { openTab } from './review-helpers';

// H8b version history: compare, restore and the item's activity. The administrator holds review.bypass, so the
// article type's review requirement does not get in the way of publishing.
test.describe.serial('version history', () => {
  const t = tag();
  const first = `alpha${t}`;
  const second = `beta${t}`;
  let id = '';
  let firstTitle = ''; // the helper appends its own tag

  test.beforeAll(async () => {
    const a = await draftArticle(`History ${first}`);
    id = a.id;
    firstTitle = a.title;
    await manage('POST', `/items/${id}/publish`, {});
    await manage('PATCH', `/items/${id}`, { locale: 'en', fields: { title: `History ${second}` } });
    await manage('POST', `/items/${id}/publish`, {});
    await expectDelivered(id, 200);
  });

  test.beforeEach(async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
  });

  test('Compare… shows the inserted and deleted words of a changed field', async ({ page }) => {
    const errors = watchErrors(page);
    await page.goto(`/admin/items/${id}`);
    await openTab(page, 'History');
    const versions = manage('GET', `/items/${id}/versions`);
    expect((await versions).items.length).toBeGreaterThanOrEqual(2);
    // Version rows are newest first: the last one is the oldest, whose title differs from the draft's.
    await page.getByRole('button', { name: 'Compare…' }).last().click();
    await expect(page).toHaveURL(/\/compare/);
    await expect(page.getByRole('heading', { name: 'Compare versions' })).toBeVisible();
    await expect(page.locator('del', { hasText: first })).toBeVisible();
    await expect(page.locator('ins', { hasText: second })).toBeVisible();
    expect(await axeViolations(page)).toEqual([]);
    expect(errors).toEqual([]);
  });

  test('Restore… writes an older version into the draft only', async ({ page }) => {
    const versions = (await manage('GET', `/items/${id}/versions`)).items as { version: number }[];
    const oldest = Math.min(...versions.map((v) => v.version));
    await page.goto(`/admin/items/${id}`);
    await openTab(page, 'History');
    // Version rows are newest first; the oldest one is last.
    await page.getByRole('button', { name: 'Restore…' }).last().click();
    const dlg = page.getByRole('dialog', { name: 'Restore version' });
    await expect(dlg).toBeVisible();
    await dlg.getByRole('button', { name: /^Restore/ }).last().click();
    await expect(page.getByRole('dialog')).toHaveCount(0);

    expect((await draftOf(id)).fields.title).toBe(firstTitle);
    await expect(page.getByLabel('Title')).toHaveValue(firstTitle);
    // Delivery still serves the published content until the next publish.
    const live = await deliver(`/items/${id}?channel=corporate_en&locale=en`);
    // (the slug was made from the first title and stays, so only the title is compared)
    expect(live.item.fields.title).toBe(`History ${second}`);
    expect(oldest).toBeGreaterThanOrEqual(1);
  });

  test('Activity lists publish and unpublish entries', async ({ page }) => {
    await manage('POST', `/items/${id}/unpublish`, {});
    await page.goto(`/admin/items/${id}`);
    await openTab(page, 'History');
    const activity = page.getByRole('list', { name: 'Activity' });
    await expect(activity).toBeVisible();
    await expect(activity.getByRole('listitem').filter({ hasText: 'unpublished the item' })).toHaveCount(1);
    await expect(activity.getByRole('listitem').filter({ hasText: /published version \d+/ })).toHaveCount(2);
  });
});
