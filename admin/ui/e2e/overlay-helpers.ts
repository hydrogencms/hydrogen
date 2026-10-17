import { expect, type Page } from '@playwright/test';
import { base, manage, tag } from './helpers';

export const richText = (text: string) => ({ type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text }] }] });

/** The text of the first paragraph of a rich text value (delivery or manage). */
export const bodyText = (body: any): string => body?.content?.[0]?.content?.[0]?.text ?? '';

/** An article published in de with a title and a body (corporate_dech lists de-CH, de, en; articles are delivered there). */
export async function publishedGermanArticle(label: string) {
  const title = `${label} ${tag()}`;
  const item = await manage('POST', '/collections/article/items', { locale: 'de', fields: { title, body: richText('Deutscher Text') } });
  await manage('POST', `/items/${item.id}/publish`, {});
  return { id: item.id as string, title };
}

/** The delivered item for de-CH through the corporate_dech channel (null on 404). */
export async function deliveredDeCH(id: string): Promise<any | null> {
  const res = await fetch(`${base}/api/deliver/v1/items/${id}?channel=corporate_dech&locale=de-CH`);
  if (res.status === 404) return null;
  if (!res.ok) throw new Error(`deliver ${id}: ${res.status}`);
  const j = await res.json();
  return { ...j.item, locale: j.item?.locale ?? j.locale };
}

/** Delivery caches are purged through the outbox: poll until the predicate holds. */
export const expectDeCH = (id: string, check: (item: any) => unknown, timeout = 15_000) =>
  expect.poll(async () => check(await deliveredDeCH(id)), { timeout }).toBe(true);

/** The editor row of a field (label text), found by the field's label. */
/** A top-level field's row (badge, Override/Reset button and widget), by its label: the field name is the label lower-cased. */
export const fieldRow = (page: Page, label: string) => page.locator(`.h-item__form [data-field="${label.toLowerCase()}"]`).first();

export async function saveEditor(page: Page) {
  await page.getByRole('button', { name: 'Save', exact: true }).first().click();
  await expect(page.getByRole('button', { name: 'Saved', exact: true }).first()).toBeVisible();
}

export async function publishEditor(page: Page) {
  await page.getByRole('button', { name: 'Publish', exact: true }).first().click();
  await page.getByRole('dialog').getByRole('button', { name: /^Publish/ }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
}
