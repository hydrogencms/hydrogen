import { expect, test } from '@playwright/test';
import { card, expand, frameOf } from './blocks-helpers';
import { adminLogin, adminPassword, draftOf, manage, signIn, tag } from './helpers';

// The H6 exit scenario on the site module (examples/fullstack): one binary serves the admin and the website; the
// frontend renders on the server, so the admin saves the draft and the frame reloads (reload mode).
const t = tag();
test.setTimeout(90_000);
test.use({ actionTimeout: 15_000 });

test('typing in the admin autosaves the draft and the website frame shows it', async ({ page }) => {
  const r = await manage('GET', '/collections/landing/items?limit=20');
  const home = r.items.find((i: any) => i.name === 'Home');
  await signIn(page, adminLogin, adminPassword);
  await page.goto(`/admin/items/${home.id}`);
  await page.getByRole('button', { name: 'Preview', exact: true }).click();
  const frame = frameOf(page);
  await expect(frame.locator('[data-h-block="hero"]').first()).toBeVisible();
  await expect(page.getByRole('status').filter({ hasText: 'Reload mode' })).toBeVisible();
  await expect(page.getByText(/saves your changes to the draft/)).toBeVisible();

  const hero = card(page, 'hero').first();
  await expand(hero);
  await hero.getByLabel(/^Heading/).fill(`Server ${t}`);
  await expect(frame.locator('[data-h-block="hero"]').first()).toContainText(`Server ${t}`);
  await expect(page.getByText('Saved', { exact: true }).first()).toBeVisible();

  const draft = await draftOf(home.id);
  expect(JSON.stringify(draft.fields.body)).toContain(`Server ${t}`);
  expect(draft.status).toBe('changed');
  expect(draft.version).toBe(1);
});
