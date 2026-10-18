import { expect, test } from '@playwright/test';
import { adminLogin, adminPassword, base, manage, signIn, watchErrors } from './helpers';
import { axeViolations, expectDelivered } from './publishing-helpers';

// H9c search on the multisite e2e setup: the admin palette and /search route on GET /manage/v1/search, and
// GET /deliver/v1/search. Published through the management token (it holds review.bypass).
test.describe.serial('search', () => {
  // Letters only and unique per run (ADMIN_E2E_ONLY runs keep the database).
  const word = 'zq' + Array.from({ length: 8 }, () => String.fromCharCode(97 + Math.floor(Math.random() * 26))).join('');
  let article: { id: string; name: string };
  let page_: { id: string; name: string };
  let draft: { id: string; name: string };

  const create = async (type: string, title: string, extra: Record<string, unknown> = {}) => {
    const item = await manage('POST', `/collections/${type}/items`, { locale: 'en', fields: { title, ...extra } });
    return { id: item.id as string, name: title };
  };

  test('1. the palette finds items of two types with one search request', async ({ page }) => {
    article = await create('article', `Solar ${word} article`);
    await manage('POST', `/items/${article.id}/publish`, {});
    page_ = await create('page', `Guide ${word} page`, { intro: `About ${word} in depth` });
    await manage('POST', `/items/${page_.id}/publish`, {});
    draft = await create('article', `Draft ${word} only`);

    const errors = watchErrors(page);
    const searches: string[] = [];
    const lists: string[] = [];
    page.on('request', (r) => {
      const u = new URL(r.url());
      if (u.pathname === '/api/manage/v1/search') searches.push(u.search);
      if (/\/collections\/[^/]+\/items$/.test(u.pathname) && u.searchParams.has('q')) lists.push(u.pathname + u.search);
    });
    await signIn(page, adminLogin, adminPassword);
    await page.keyboard.press('ControlOrMeta+k');
    const input = page.getByRole('combobox');
    await expect(input).toBeVisible();
    await input.fill(word);
    const list = page.getByRole('listbox');
    await expect(list.getByRole('option', { name: new RegExp(article.name) })).toBeVisible();
    await expect(list.getByRole('option', { name: new RegExp(page_.name) })).toBeVisible();
    await expect(list.getByRole('option', { name: new RegExp(article.name) })).toContainText(/article/i);
    await expect(list.getByRole('option', { name: new RegExp(page_.name) })).toContainText(/page/i);
    // The working projection finds the draft-only item too.
    await expect(list.getByRole('option', { name: new RegExp(draft.name) })).toBeVisible();

    await page.waitForTimeout(500);
    expect(searches.length, searches.join(' ')).toBe(1);
    expect(lists, 'no per-type list requests').toEqual([]);
    expect(errors).toEqual([]);
  });

  test('2. the /search route shows a highlighted snippet and passes axe', async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
    await page.keyboard.press('ControlOrMeta+k');
    await page.getByRole('combobox').fill(word);
    await page.getByRole('option', { name: /search for/i }).click();
    await expect(page).toHaveURL(new RegExp(`/admin/search\\?q=${word}`));
    const items = page.locator('.h-search__item');
    await expect(items.filter({ hasText: article.name })).toBeVisible();
    await expect(items.filter({ hasText: page_.name })).toBeVisible();
    const mark = page.locator('.h-search__snippet mark.h-mark').first();
    await expect(mark).toBeVisible();
    expect((await mark.textContent())?.toLowerCase()).toBe(word);
    expect(await axeViolations(page)).toEqual([]);
  });

  test('3. delivery search returns the published article, highlighted, and not the draft', async () => {
    await expectDelivered(article.id, 200);
    const url = (q: string, extra = '') => `${base}/api/deliver/v1/search?channel=corporate_en&locale=en&q=${encodeURIComponent(q)}${extra}`;
    let body: any;
    await expect
      .poll(
        async () => {
          const res = await fetch(url(word));
          if (res.status !== 200) return `status ${res.status}`;
          body = await res.json();
          return (body.items ?? []).some((i: any) => i.id === article.id) ? 'found' : 'missing';
        },
        { timeout: 15_000 },
      )
      .toBe('found');
    expect(body.total).toBeGreaterThanOrEqual(1);
    const ids = body.items.map((i: any) => i.id);
    expect(body.hits.map((h: any) => h.id)).toEqual(ids);
    expect(ids).not.toContain(draft.id);
    const hit = body.hits.find((h: any) => h.id === article.id);
    expect(hit.highlights.text ?? hit.highlights.name).toContain('<mark>');
    expect(JSON.stringify(hit.highlights)).toContain(`<mark>${word}</mark>`);
    expect(body.hits[0].highlights.text ?? body.hits[0].highlights.name).toContain('<mark>');

    const bad = await fetch(url(word, '&type=nonexistent'));
    await bad.arrayBuffer();
    expect(bad.status).toBe(400);
  });
});
