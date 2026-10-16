import { expect, test } from '@playwright/test';
import { card, expand, frameOf } from './blocks-helpers';
import { adminLogin, adminPassword, draftOf, manage, signIn, tag } from './helpers';

// The H6 exit scenario on the site module (examples/fullstack): one binary serves the admin and the website; the
// frontend renders on the server, so the admin relays the unsaved document and the frame reloads (reload mode).
const t = tag();
test.setTimeout(90_000);
test.use({ actionTimeout: 15_000 });

test('typing in the admin shows in the website frame without saving the draft', async ({ page }) => {
  const r = await manage('GET', '/collections/landing/items?limit=20');
  const home = r.items.find((i: any) => i.name === 'Home');
  await signIn(page, adminLogin, adminPassword);
  await page.goto(`/admin/items/${home.id}`);
  await page.getByRole('button', { name: 'Preview', exact: true }).click();
  const frame = frameOf(page);
  // The website is on the admin's origin, so the frame is locked: no allow-same-origin.
  const sandbox = await page.locator('iframe[title^="Preview of"]').getAttribute('sandbox');
  expect(sandbox).toContain('allow-scripts');
  expect(sandbox).not.toContain('allow-same-origin');
  await expect(frame.locator('[data-h-block="hero"]').first()).toBeVisible();
  await expect(page.getByRole('status').filter({ hasText: 'Reload mode' })).toBeVisible();
  await expect(page.getByText(/saves your changes to the draft/)).toHaveCount(0);

  const hero = card(page, 'hero').first();
  await expand(hero);
  await hero.getByLabel(/^Heading/).fill(`Server ${t}`);
  await expect(frame.locator('[data-h-block="hero"]').first()).toContainText(`Server ${t}`);
  await expect(page.getByText('Unsaved changes shown')).toBeVisible();

  const before = await draftOf(home.id);
  expect(JSON.stringify(before.fields.body)).not.toContain(`Server ${t}`);
  await page.getByRole('button', { name: 'Save', exact: true }).first().click();
  await expect(page.getByRole('button', { name: 'Saved', exact: true }).first()).toBeVisible();
  const draft = await draftOf(home.id);
  expect(JSON.stringify(draft.fields.body)).toContain(`Server ${t}`);
  expect(draft.status).toBe('changed');
});

test('canvas: inline edit of the title in the locked site frame commits and the reloaded frame shows it', async ({ page }) => {
  const r = await manage('GET', '/collections/page/items?limit=20');
  const about = r.items.find((i: any) => i.name === 'About');
  await signIn(page, adminLogin, adminPassword);
  await page.goto(`/admin/items/${about.id}`);
  await page.getByRole('button', { name: 'Preview', exact: true }).click();
  const frame = frameOf(page);
  const sandbox = await page.locator('iframe[title^="Preview of"]').getAttribute('sandbox');
  expect(sandbox).not.toContain('allow-same-origin');
  await expect(page.getByRole('status').filter({ hasText: 'Reload mode' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Edit on canvas' })).toHaveAttribute('aria-pressed', 'true');
  const title = frame.locator('h1.page-title');
  await expect(title).toBeVisible();

  await title.dblclick();
  await expect(frame.locator('[contenteditable]')).toBeVisible();
  await page.keyboard.press('ControlOrMeta+a');
  await page.keyboard.type(`About ${t}`);
  await page.keyboard.press('Enter');
  await expect(page.getByRole('textbox', { name: 'Title' })).toHaveValue(`About ${t}`);
  // The relayed document, rendered by the site module in a reloaded frame; the draft is untouched.
  await expect(frame.locator('h1.page-title')).toHaveText(`About ${t}`);
  await expect(frame.locator('[contenteditable]')).toHaveCount(0);
  expect((await draftOf(about.id)).fields.title).not.toBe(`About ${t}`);
});
