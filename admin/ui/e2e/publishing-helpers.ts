import { expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { base, manage, tag } from './helpers';

/** The HTTP status of an item on the delivery API (corporate_en delivers articles, pages and landings). */
export async function deliveredStatus(id: string, channel = 'corporate_en', locale = 'en') {
  const res = await fetch(`${base}/api/deliver/v1/items/${id}?channel=${channel}&locale=${locale}`);
  await res.arrayBuffer();
  return res.status;
}

/** Delivery caches are purged asynchronously through the outbox: poll until the status settles. */
export const expectDelivered = (id: string, status: number, timeout = 15_000) =>
  expect.poll(() => deliveredStatus(id), { timeout }).toBe(status);

/** A draft article (never published) with a unique title. */
export async function draftArticle(label: string, fields: Record<string, unknown> = {}) {
  const title = `${label} ${tag()}`;
  const item = await manage('POST', '/collections/article/items', { locale: 'en', fields: { title, ...fields } });
  return { id: item.id as string, title };
}

/** A published article. */
export async function publishedArticle(label: string) {
  const a = await draftArticle(label);
  await manage('POST', `/items/${a.id}/publish`, {});
  return a;
}

/** Serious and critical axe violations (WCAG 2.x A/AA and best practices), formatted for the assertion. */
export async function axeViolations(page: Page) {
  const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'best-practice']).analyze();
  expect(result.passes.length, 'axe ran').toBeGreaterThan(10);
  return result.violations
    .filter((v) => v.impact === 'serious' || v.impact === 'critical')
    .map((v) => `${v.id} (${v.impact}): ${v.help}\n${v.nodes.slice(0, 3).map((n) => '    ' + n.target.join(' ')).join('\n')}`);
}
