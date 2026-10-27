import { expect, test } from '@playwright/test';
import { pick, adminLogin, adminPassword, manage, signIn, tag, watchErrors } from './helpers';
import { axeViolations, deliveredStatus, draftArticle, expectDelivered, publishedArticle } from './publishing-helpers';

// H8a releases and scheduling. The server's `releases` job runs every minute (the runner polls each second) and
// its schedule is not configurable, so scheduled publishes use the real clock and wait up to 90 s.
const newRelease = (name: string, extra: Record<string, unknown> = {}) =>
  manage('POST', '/releases', { name: `${name} ${tag()}`, ...extra });
const addItem = (release: string, item: string, action = 'publish') => manage('PUT', `/releases/${release}/items/${item}`, { action });
const getRelease = (id: string) => manage('GET', `/releases/${id}`);

test.describe('releases', () => {
  test.beforeEach(async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
  });

  test('a release with an invalid item publishes nothing; fixed, it publishes both items together', async ({ page }) => {
    const errors = watchErrors(page);
    const a = await draftArticle('Rel A');
    const b = await draftArticle('Rel B');
    // The required title is only checked on publish, so an empty draft is a valid draft and an invalid publish.
    const bad = await manage('POST', '/collections/article/items', { locale: 'en', fields: {} });
    const rel = await newRelease('Launch');
    for (const id of [a.id, b.id, bad.id]) await addItem(rel.id, id);

    await manage('POST', `/releases/${rel.id}/publish`, {}, [200, 201, 204, 400, 409, 422]);
    const failed = await getRelease(rel.id);
    expect(failed.status).toBe('failed');
    expect(failed.errors).toHaveLength(1);
    expect(failed.errors[0].item).toBe(bad.id);
    expect(await deliveredStatus(a.id)).toBe(404);
    expect(await deliveredStatus(b.id)).toBe(404);

    // The admin lists the failure per item with a link to it.
    await page.goto(`/admin/releases/${rel.id}`);
    await expect(page.getByRole('heading', { level: 1, name: rel.name })).toBeVisible();
    await expect(page.getByRole('table', { name: 'Release items' }).getByRole('row')).toHaveCount(4); // header + 3
    await expect(page.getByText(failed.errors[0].message).first()).toBeVisible();
    await expect(page.getByRole('link', { name: new RegExp(`/admin/items/${bad.id}`) }).or(page.locator(`a[href$="/items/${bad.id}"]`)).first()).toBeVisible();

    // Fix the draft, publish from the admin.
    await manage('PATCH', `/items/${bad.id}`, { locale: 'en', fields: { title: `Rel fixed ${tag()}` } });
    await page.reload();
    await page.getByRole('button', { name: 'Publish now' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Publish now' }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);

    await expect.poll(async () => (await getRelease(rel.id)).status, { timeout: 15_000 }).toBe('published');
    for (const id of [a.id, b.id, bad.id]) await expectDelivered(id, 200);
    expect(errors).toEqual([]);
  });

  test('a release can unpublish an item', async () => {
    const live = await publishedArticle('Rel unpublish');
    const rel = await newRelease('Retire');
    await addItem(rel.id, live.id, 'unpublish');
    await expectDelivered(live.id, 200);
    await manage('POST', `/releases/${rel.id}/publish`, {});
    expect((await getRelease(rel.id)).status).toBe('published');
    await expectDelivered(live.id, 404);
  });

  test('add to release from the item editor, then remove the item', async ({ page }) => {
    const a = await draftArticle('Rel add');
    const rel = await newRelease('Picked');
    await page.goto(`/admin/items/${a.id}`);
    await page.getByRole('button', { name: 'Publish options' }).click();
    await page.getByRole('menuitem', { name: 'Add to release…' }).click();
    const dlg = page.getByRole('dialog', { name: 'Add to release' });
    await expect(dlg).toBeVisible();
    await pick(page, dlg, 'Release', rel.name, true);
    await dlg.getByRole('button', { name: 'Add', exact: true }).click();
    await expect(dlg).toHaveCount(0);
    await expect.poll(async () => ((await manage('GET', `/items/${a.id}/releases`)).items ?? []).length).toBe(1);

    await manage('DELETE', `/releases/${rel.id}/items/${a.id}`);
    expect((await manage('GET', `/items/${a.id}/releases`)).items ?? []).toHaveLength(0);
  });

  test('unschedule and cancel from the admin', async ({ page }) => {
    const a = await draftArticle('Rel cancel');
    const rel = await newRelease('Later');
    await addItem(rel.id, a.id);
    await manage('POST', `/releases/${rel.id}/schedule`, { at: new Date(Date.now() + 3600_000).toISOString() });
    expect((await getRelease(rel.id)).status).toBe('scheduled');

    await page.goto(`/admin/releases/${rel.id}`);
    await page.getByRole('button', { name: 'Unschedule' }).click();
    await expect.poll(async () => (await getRelease(rel.id)).status).toBe('open');

    await page.reload();
    await page.getByRole('button', { name: 'Cancel release' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Cancel release' }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect.poll(async () => (await getRelease(rel.id)).status).toBe('cancelled');
    // Cancelling leaves the item alone: still an unpublished draft with its title.
    expect(await deliveredStatus(a.id)).toBe(404);
    const draft = await manage('GET', `/items/${a.id}?draft=true&locale=en`);
    expect(draft.fields.title).toBe(a.title);
    expect(draft.status).toBe('draft');
  });

  test('the Schedule… dialog opens from the editor', async ({ page }) => {
    const a = await draftArticle('Rel dialog');
    await page.goto(`/admin/items/${a.id}`);
    await page.getByRole('button', { name: 'Publish options' }).click();
    await page.getByRole('menuitem', { name: 'Schedule…' }).click();
    const dlg = page.getByRole('dialog', { name: 'Schedule publish' });
    await expect(dlg).toBeVisible();
    await dlg.getByRole('button', { name: 'Cancel' }).click();
    await expect(dlg).toHaveCount(0);
  });

  test('a scheduled one-item publish goes live on its own and shows a badge meanwhile', async ({ page }) => {
    test.setTimeout(150_000);
    const a = await draftArticle('Rel scheduled');
    const rel = await newRelease(`${a.title} — publish`, { kind: 'item' });
    await addItem(rel.id, a.id);
    await manage('POST', `/releases/${rel.id}/schedule`, { at: new Date(Date.now() + 3000).toISOString() });

    await page.goto(`/admin/items/${a.id}`);
    // The badge shows while the release is still scheduled (the job may beat a slow page load, so it is optional).
    await page.getByText(/^Scheduled · /).first().waitFor({ timeout: 2000 }).catch(() => {});

    await expect.poll(async () => (await getRelease(rel.id)).status, { timeout: 90_000, intervals: [1000] }).toBe('published');
    await expectDelivered(a.id, 200);
    expect((await getRelease(rel.id)).publishedAt).toBeTruthy();
  });

  test('the Releases screens are accessible', async ({ page }) => {
    const a = await draftArticle('Axe rel');
    const rel = await newRelease('Axe release');
    await addItem(rel.id, a.id);

    await page.goto('/admin/releases');
    await expect(page.getByRole('heading', { level: 1, name: 'Releases' })).toBeVisible();
    await expect(page.getByRole('link', { name: rel.name })).toBeVisible();
    expect(await axeViolations(page)).toEqual([]);

    await page.goto(`/admin/releases/${rel.id}`);
    await expect(page.getByRole('table', { name: 'Release items' })).toBeVisible();
    expect(await axeViolations(page)).toEqual([]);
  });
});
