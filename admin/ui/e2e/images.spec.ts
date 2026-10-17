import { expect, test } from '@playwright/test';
import { adminLogin, adminPassword, base, manage, manageToken, signIn, tag } from './helpers';
import { draftArticle, expectDelivered } from './publishing-helpers';
import { assetURL, makeSplitPNG, pngPixel, pngSize, sha256 } from './media-helpers';

// H9b image transforms on the multisite e2e setup: modules.images has a `card` preset
// ({width: 640, height: 360, fit: cover, widths: [320]}). Published through the management token (review.bypass).
test.describe.serial('images', () => {
  const rnd = () => Math.floor(Math.random() * 256);
  // Top and bottom halves far apart in colour; the unique low channel keeps the bytes (and sha) distinct per run.
  const top: [number, number, number] = [220, 20, rnd() % 40];
  const bottom: [number, number, number] = [20, 20, 200 + (rnd() % 50)];
  const png = makeSplitPNG(1200, 800, top, bottom);
  const sha = sha256(png);
  const name = `e2e-img-${tag()}.png`;
  let assetId = '';
  let article: { id: string; title: string };
  let variant: { url: string; width: number; height: number; type: string; srcset: string };

  test('1. upload a 1200x800 PNG, create the image with a focal point, publish', async () => {
    const form = new FormData();
    form.append('type', 'image');
    form.append('file', new Blob([new Uint8Array(png)], { type: 'image/png' }), name);
    const res = await fetch(`${base}/api/manage/v1/uploads`, { method: 'POST', headers: { Authorization: `Bearer ${manageToken}` }, body: form });
    expect(res.status).toBeLessThan(300);
    const file = await res.json();
    expect(file).toMatchObject({ id: sha, width: 1200, height: 800 });
    const item = await manage('POST', '/collections/image/items', { locale: 'en', fields: { file: { ...file, focal: { x: 0.25, y: 0.9 } }, alt: `Split ${tag()}` } });
    assetId = item.id;
    await manage('POST', `/items/${assetId}/publish`, {});
  });

  test('2. attach it as an article hero; delivery lists the card variant', async () => {
    article = await draftArticle('Images article', { hero: assetId });
    await manage('POST', `/items/${article.id}/publish`, {});
    await expectDelivered(article.id, 200);
    const get = async () => {
      const body = await (await fetch(`${base}/api/deliver/v1/items/${article.id}?channel=corporate_en&locale=en`)).json();
      return (body.fields ?? body.item?.fields ?? body).hero;
    };
    await expect.poll(async () => (await get())?.variants?.card?.url, { timeout: 15_000 }).toBeTruthy();
    const hero = await get();
    expect(hero.id).toBe(assetId);
    variant = hero.variants.card;
    expect(variant).toMatchObject({ width: 640, height: 360, type: 'image/png' });
    expect(variant.url).toContain('/images/card/');
    expect(variant.url).toContain(`/${sha}/`);
    expect(variant.url).toContain(`/640/2590/${sha}/`);
    expect(variant.srcset).toContain(' 320w');
    expect(variant.srcset).toContain(' 640w');
  });

  test('3. the variant is generated once, then served from storage', async () => {
    const url = assetURL(variant.url);
    const first = await fetch(url);
    expect(first.status).toBe(200);
    expect(first.headers.get('content-type')).toMatch(/^image\/png/);
    expect(first.headers.get('hydrogen-image')).toBe('generated');
    expect(first.headers.get('cache-control')).toMatch(/immutable/);
    const bytes = Buffer.from(await first.arrayBuffer());
    expect(pngSize(bytes)).toEqual({ width: 640, height: 360 });
    // The 16:9 crop of the 1200x800 source is vertical; focal y 0.9 pushes it to the bottom, so the centre is the bottom colour.
    const [r, g, b] = pngPixel(bytes, 320, 180);
    expect(Math.abs(r - bottom[0]) + Math.abs(g - bottom[1]) + Math.abs(b - bottom[2])).toBeLessThan(12);
    const [tr, , tb] = pngPixel(bytes, 320, 2);
    expect(Math.abs(tr - top[0]) + Math.abs(tb - top[2])).toBeLessThan(12);

    const second = await fetch(url);
    expect(second.status).toBe(200);
    expect(second.headers.get('hydrogen-image')).toBe('stored');
    expect(pngSize(Buffer.from(await second.arrayBuffer()))).toEqual({ width: 640, height: 360 });
  });

  test('4. the smaller srcset width is served too', async () => {
    const small = assetURL(variant.url).replace(/\/640\//, '/320/');
    const res = await fetch(small);
    expect(res.status).toBe(200);
    expect(pngSize(Buffer.from(await res.arrayBuffer()))).toEqual({ width: 320, height: 180 });
  });

  test('5. an unknown width is 404; a stale version redirects to the current URL', async () => {
    const u = new URL(assetURL(variant.url));
    const seg = u.pathname.split('/'); // '', images, card, v, w, focal, sha, name
    const at = seg.indexOf('card');
    expect(seg[at + 2]).toBe('640');

    const badW = [...seg];
    badW[at + 2] = '999';
    expect((await fetch(new URL(badW.join('/'), u))).status).toBe(404);

    const badF = [...seg];
    expect(badF[at + 3]).toBe('2590');
    badF[at + 3] = '0101';
    expect((await fetch(new URL(badF.join('/'), u))).status).toBe(404);

    const stale = [...seg];
    stale[at + 1] = '00000000';
    const res = await fetch(new URL(stale.join('/'), u), { redirect: 'manual' });
    expect(res.status).toBe(302);
    expect(new URL(res.headers.get('location')!, u).toString()).toBe(u.toString());
  });

  test('6. the Assets screen shows thumbnails from /images/admin/', async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
    await page.goto('/admin/assets');
    await expect(page.getByText(name).first()).toBeVisible();
    await expect(page.locator(`img[src*="${sha}"]`).first()).toHaveAttribute('src', /\/images\/admin\/[^/]+\/\d+\/c\//);
  });
});
