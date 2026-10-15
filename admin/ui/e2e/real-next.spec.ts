import { expect, test } from '@playwright/test';
import { add, card, expand, frameOf, rootAdd } from './blocks-helpers';
import { adminLogin, adminPassword, draftOf, manage, signIn, tag } from './helpers';

// The H6 exit scenario on the real Next.js starter (starters/next) against examples/starter: the editor types in the
// admin and the page in the frame follows without a save, across origins (live mode).
const t = tag();
test.setTimeout(90_000);
test.use({ actionTimeout: 15_000 });
test.describe.configure({ mode: 'serial' });

async function first(type: string, name?: string) {
  const r = await manage('GET', `/collections/${type}/items?limit=20`);
  const hit = r.items.find((i: any) => !name || i.name === name);
  if (!hit) throw new Error(`no ${type} item ${name ?? ''} in the fixture`);
  return hit as { id: string; name: string };
}

test('a fixture page: heading and a new block appear in the Next.js frame while typing', async ({ page }) => {
  const home = await first('landing', 'Home');
  await signIn(page, adminLogin, adminPassword);
  await page.goto(`/admin/items/${home.id}`);
  await page.getByRole('button', { name: 'Preview', exact: true }).click();
  const frame = frameOf(page);
  await expect(frame.locator('[data-h-block="hero"]').first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole('status').filter({ hasText: 'Live' })).toBeVisible({ timeout: 30_000 });

  const hero = card(page, 'hero').first();
  await expand(hero);
  await hero.getByLabel(/^Heading/).fill(`Typed ${t}`);
  await expect(frame.locator('[data-h-block="hero"] .hero-title').first()).toHaveText(`Typed ${t}`);

  await add(rootAdd(page), /^Callout/);
  await card(page, 'callout').last().getByLabel(/^Text/).fill(`Callout ${t}`);
  await expect(frame.locator('[data-h-block="callout"]').last()).toContainText(`Callout ${t}`);

  // Nothing was saved: the draft on the server still has the old heading.
  const draft = await draftOf(home.id);
  expect(JSON.stringify(draft.fields.body)).not.toContain(`Typed ${t}`);
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeEnabled();
});

test('a post: the title appears in the Next.js frame while typing', async ({ page }) => {
  const post = await first('post');
  await signIn(page, adminLogin, adminPassword);
  await page.goto(`/admin/items/${post.id}`);
  await page.getByRole('button', { name: 'Preview', exact: true }).click();
  const frame = frameOf(page);
  await expect(frame.locator('h1').first()).toContainText(post.name, { timeout: 30_000 });
  await page.getByRole('textbox', { name: 'Title' }).fill(`Post ${t}`);
  await expect(frame.locator('h1').first()).toContainText(`Post ${t}`);
});
