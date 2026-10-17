import { expect, test, type Page } from '@playwright/test';
import { adminLogin, adminPassword, base, manage, manageToken, signIn, tag, watchErrors } from './helpers';
import { axeViolations, draftArticle, expectDelivered } from './publishing-helpers';
import { assetURL, randomPNG, sha256, tinyPDF } from './media-helpers';

// H9a media core on the multisite e2e schema (an `image` upload type; `article.hero` is an asset field).
// Contract: planning/design/media-h9a.md. Published through the management token (it holds review.bypass).
test.describe.serial('media', () => {
  const png = randomPNG();
  const name = `e2e-${tag()}.png`;
  const alt = `A red square ${tag()}`;
  let assetId = '';
  let article: { id: string; title: string };

  const upload = (page: Page, file: { name: string; mimeType: string; buffer: Buffer }) =>
    page.locator('input[type=file]').first().setInputFiles(file);

  const idOf = (v: any) => (v && typeof v === 'object' ? v.id : v);

  test('1. upload an image in Assets: it appears with its dimensions', async ({ page }) => {
    const errors = watchErrors(page);
    await signIn(page, adminLogin, adminPassword);
    await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Assets' }).click();
    await expect(page).toHaveURL(/\/admin\/assets/);
    await upload(page, { name, mimeType: 'image/png', buffer: png });
    const card = page.getByText(name).first();
    await expect(card).toBeVisible();
    await expect(page.getByText(/4\s*[×x]\s*3/).first()).toBeVisible();

    const list = await manage('GET', `/collections/image/items?q=${encodeURIComponent(name)}`);
    expect(list.items).toHaveLength(1);
    assetId = list.items[0].id;
    const item = await manage('GET', `/items/${assetId}?draft=true&locale=en`);
    expect(item.fields.file).toMatchObject({ id: sha256(png), name, mime: 'image/png', size: png.length, width: 4, height: 3 });
    expect(errors).toEqual([]);
  });

  test('2. details: alt text, folder, tag and a keyboard focal point', async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
    await page.goto('/admin/assets');
    await page.getByText(name).first().click();
    // Alt text is per language (one field each); this one is English.
    await page.getByRole('group', { name: 'Language: en', exact: true }).getByLabel('Alt', { exact: true }).fill(alt);
    await page.getByLabel('Folder', { exact: true }).fill('/marketing/2026');
    const tags = page.getByLabel('Tags', { exact: true });
    await tags.fill('hero');
    await tags.press('Enter');

    const focal = page.getByRole('group', { name: 'Focal point' }).or(page.getByRole('slider', { name: 'Focal point' })).or(page.getByLabel('Focal point')).first();
    await focal.focus();
    await page.keyboard.press('ArrowRight');
    await page.keyboard.press('ArrowRight');
    await page.keyboard.press('ArrowDown');
    await page.getByRole('button', { name: /^Save/ }).click();

    await expect.poll(async () => (await manage('GET', `/items/${assetId}?draft=true&locale=en`)).fields.alt).toBe(alt);
    const item = await manage('GET', `/items/${assetId}?draft=true&locale=en`);
    expect(item.fields.folder).toBe('/marketing/2026');
    expect(item.fields.tags).toEqual(['hero']);
    expect(item.fields.file.focal.x).toBeGreaterThan(0.5);
    expect(item.fields.file.focal.y).toBeGreaterThan(0.5);
    expect(item.fields.file.focal.x).toBeLessThanOrEqual(1);
    // the server still owns the metadata
    expect(item.fields.file).toMatchObject({ id: sha256(png), width: 4, height: 3, mime: 'image/png' });

    await manage('POST', `/items/${assetId}/publish`, {});
  });

  test('3. choose it in an article asset field; publish both; delivery expands it', async ({ page }) => {
    article = await draftArticle('Media article');
    await signIn(page, adminLogin, adminPassword);
    await page.goto(`/admin/items/${article.id}`);
    await page.getByRole('button', { name: 'Choose…' }).first().click();
    const dlg = page.getByRole('dialog', { name: 'Choose an asset' });
    await expect(dlg).toBeVisible();
    expect(await axeViolations(page)).toEqual([]);
    // a card click chooses the asset and closes the dialog
    await dlg.getByRole('button', { name: new RegExp(name.replace(/\./g, '\\.')) }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(page.getByText(name).first()).toBeVisible();
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect.poll(async () => idOf((await manage('GET', `/items/${article.id}?draft=true&locale=en`)).fields.hero)).toBe(assetId);

    await manage('POST', `/items/${article.id}/publish`, {});
    await expectDelivered(article.id, 200);
    const res = await fetch(`${base}/api/deliver/v1/items/${article.id}?channel=corporate_en&locale=en`);
    expect(res.status).toBe(200);
    const body = await res.json();
    const hero = (body.fields ?? body.item?.fields ?? body).hero;
    expect(hero).toMatchObject({ id: assetId, name, mime: 'image/png', size: png.length, width: 4, height: 3 });
    expect(hero.url).toContain(sha256(png));
    expect(hero.fields.alt).toBe(alt);
    expect(hero.focal.x).toBeGreaterThan(0.5);
    (globalThis as any).__heroURL = hero.url;
  });

  test('4. GET url serves the bytes, content type and immutable caching', async () => {
    const url = (globalThis as any).__heroURL as string;
    const res = await fetch(assetURL(url));
    expect(res.status).toBe(200);
    expect(res.headers.get('content-type')).toMatch(/^image\/png/);
    expect(res.headers.get('cache-control')).toMatch(/immutable/);
    expect(res.headers.get('cache-control')).toMatch(/max-age=31536000/);
    expect(res.headers.get('x-content-type-options')).toBe('nosniff');
    expect(res.headers.get('etag')).toContain(sha256(png));
    expect(Buffer.from(await res.arrayBuffer()).equals(png)).toBe(true);
  });

  test('5. Used by lists the article; axe is clean on the library', async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
    await page.goto('/admin/assets');
    await page.getByText(name).first().click();
    const used = page.getByRole('list', { name: 'Used by' }).or(page.getByRole('region', { name: 'Used by' })).first();
    await expect(used).toContainText(article.title);
    expect(await axeViolations(page)).toEqual([]);
  });

  test('6. uploading the same file again dedupes', async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
    await page.goto('/admin/assets');
    await upload(page, { name, mimeType: 'image/png', buffer: png });
    await expect(page.getByText(/already (exists|uploaded)|duplicate/i).first()).toBeVisible();
    const list = await manage('GET', `/collections/image/items?q=${encodeURIComponent(name)}`);
    expect(list.items).toHaveLength(1);
    // the store holds one object: the upload endpoint reports the same sha
    const form = new FormData();
    form.append('file', new Blob([new Uint8Array(png)], { type: 'image/png' }), name);
    const res = await fetch(`${base}/api/manage/v1/uploads`, { method: 'POST', headers: { Authorization: `Bearer ${manageToken}` }, body: form });
    expect(res.status).toBeLessThan(300);
    expect((await res.json()).id).toBe(sha256(png));
  });

  test('7. a PDF uploads and is served inline with its type', async ({ page }) => {
    const t = tag();
    const pdf = tinyPDF(t);
    const pdfName = `e2e-${t}.pdf`;
    await signIn(page, adminLogin, adminPassword);
    await page.goto('/admin/assets');
    await upload(page, { name: pdfName, mimeType: 'application/pdf', buffer: pdf });
    await expect(page.getByText(pdfName).first()).toBeVisible();
    const find = () => manage('GET', `/collections/image/items?q=${encodeURIComponent(pdfName)}`);
    await expect.poll(async () => (await find()).items.length).toBe(1);
    const item = await manage('GET', `/items/${(await find()).items[0].id}?draft=true&locale=en`);
    expect(item.fields.file.mime).toBe('application/pdf');
  });

  test('8. a trashed asset shows as null; purging it clears the field', async () => {
    const hero = async () => {
      const body = await (await fetch(`${base}/api/deliver/v1/items/${article.id}?channel=corporate_en&locale=en`)).json();
      return (body.fields ?? body.item?.fields ?? body).hero;
    };
    await manage('DELETE', `/items/${assetId}`);
    await expect.poll(async () => (await fetch(`${base}/api/deliver/v1/items/${assetId}?channel=corporate_en&locale=en`)).status, { timeout: 15_000 }).toBe(404);
    await expect.poll(hero, { timeout: 15_000 }).toBeNull();
    // the reference stays while the asset can be restored
    expect(idOf((await manage('GET', `/items/${article.id}?draft=true&locale=en`)).fields.hero)).toBe(assetId);
    // default on_delete is nullify: the purge clears the article's hero instead of breaking it
    await manage('DELETE', `/items/${assetId}?permanent=true`);
    expect(idOf((await manage('GET', `/items/${article.id}?draft=true&locale=en`)).fields.hero) ?? null).toBeNull();
  });
});
