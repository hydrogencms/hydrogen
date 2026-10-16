import { expect, test, type Page } from '@playwright/test';
import { adminLogin, adminPassword, draftOf, manage, signIn, tag } from './helpers';

// Share links and relayed previews (H7s). The editor creates a link in the "Share preview" dialog, a second browser
// context without an admin session opens it, and what the editor types shows up there within seconds from the
// relay, without a save. Revoking sends the viewer into the refusal (401).
//   starter:   examples/starter + the fixture preview host (live mode: the bridge's viewer mode repaints the page)
//   fullstack: examples/fullstack, the site module (reload mode: the viewer page reloads)
const t = tag();
const fullstack = process.env.E2E_PROJECT === 'fullstack';
test.setTimeout(90_000);
test.use({ actionTimeout: 15_000 });
test.describe.configure({ mode: 'serial' });

const viewerTitle = (page: Page) => page.locator(fullstack ? 'h1.page-title' : '#title');
const SEEN = { timeout: 5_000 };

async function target() {
  if (!fullstack) {
    const id = (await manage('POST', '/collections/landing/items', { locale: 'en', fields: { title: `Share ${t}` } })).id as string;
    return { id, title: `Share ${t}` };
  }
  const r = await manage('GET', '/collections/page/items?limit=20');
  const about = r.items.find((i: any) => i.name === 'About');
  // Earlier specs edit About unsaved; a page closed by the test runner may leave its relayed draft and its
  // collaboration session (unsaved edits) behind until they time out: drop both.
  await manage('DELETE', `/preview-relay/${about.id}?locale=en`);
  const j = await manage('POST', `/collab/items/${about.id}/join`, { locale: 'en', edit: true });
  await manage('POST', `/collab/items/${about.id}/reset`, { client: j.client });
  return { id: about.id as string, title: (await draftOf(about.id)).fields.title as string };
}

test('a share link follows the editor live and is refused after revoke', async ({ page, browser }) => {
  const { id, title } = await target();
  await signIn(page, adminLogin, adminPassword);
  await page.goto(`/admin/items/${id}`);
  await page.getByRole('button', { name: 'Preview', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: fullstack ? 'Reload mode' : 'Live' })).toBeVisible();

  // Create the link.
  await page.getByRole('button', { name: 'Share', exact: true }).click();
  const dlg = page.getByRole('dialog', { name: 'Share preview' });
  await expect(dlg.getByText('Anyone with the link can view this draft until it expires or is revoked.')).toBeVisible();
  await dlg.getByRole('button', { name: 'Create link' }).click();
  const input = dlg.getByRole('textbox', { name: 'Share link', exact: true });
  await expect(dlg.getByRole('button', { name: 'Copy link' })).toBeVisible();
  const url = await input.inputValue();
  expect(url).toContain('preview=hps_');
  await expect(dlg.getByRole('list', { name: 'Share links' }).getByRole('listitem')).toHaveCount(1);
  await page.keyboard.press('Escape');
  await expect(dlg).toBeHidden();

  // The viewer: no admin session, a context of its own.
  const ctx = await browser.newContext();
  try {
    const viewer = await ctx.newPage();
    const res = await viewer.goto(url);
    expect(res?.status()).toBe(200);
    await expect(viewerTitle(viewer)).toHaveText(title);

    // Typing reaches the viewer without a save.
    const field = page.getByRole('textbox', { name: 'Title' });
    await field.fill(`${title} one`);
    await expect(viewerTitle(viewer)).toHaveText(`${title} one`, SEEN);
    await field.fill(`${title} two`);
    await expect(viewerTitle(viewer)).toHaveText(`${title} two`, SEEN);
    expect((await draftOf(id)).fields.title).toBe(title);

    // Revoke: the viewer reloads into the refusal.
    await page.getByRole('button', { name: 'Share', exact: true }).click();
    const list = dlg.getByRole('list', { name: 'Share links' });
    const refused = viewer.waitForResponse((r) => r.request().isNavigationRequest() && r.status() === 401, { timeout: 15_000 });
    // Revoke asks with the browser's confirm().
    page.once('dialog', (d) => void d.accept());
    await list.getByRole('button', { name: 'Revoke' }).click();
    await refused;
    await expect(list.getByText('Revoked', { exact: true })).toBeVisible();
    await page.keyboard.press('Escape');

    // The link stays dead on a fresh load.
    expect((await viewer.goto(url))?.status()).toBe(401);
  } finally {
    await ctx.close();
  }
  // The editor never saved anything.
  expect((await draftOf(id)).fields.title).toBe(title);
});
