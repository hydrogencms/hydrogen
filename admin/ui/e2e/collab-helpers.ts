import { expect, type Browser, type BrowserContext, type Locator, type Page } from '@playwright/test';
import { manage, signIn, tag } from './helpers';
import { editorUser } from './review-helpers';

export type Editor = Awaited<ReturnType<typeof editorUser>>;
export interface Session { ctx: BrowserContext; page: Page; user: Editor }

/** A page item (no review) with known fields, never published. */
export async function collabItem(label: string, fields: Record<string, unknown> = {}) {
  const title = `${label} ${tag()}`;
  const item = await manage('POST', '/collections/page/items', { locale: 'en', fields: { title, ...fields } });
  return { id: item.id as string, title };
}

/** An article with a rich text body, for the rich text co-editing case. */
export async function collabArticle(label: string, text = 'Body text') {
  const title = `${label} ${tag()}`;
  const body = { type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text }] }] };
  const item = await manage('POST', '/collections/article/items', { locale: 'en', fields: { title, body } });
  return { id: item.id as string, title };
}

/**
 * A new browser context signed in as `user`, with the item's editor open and joined: the form says it is live
 * (`data-collab`) once the session joined and the Yjs binding took over.
 */
export async function openSession(browser: Browser, user: Editor, itemId: string): Promise<Session> {
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  if (process.env.E2E_DEBUG) {
    page.on('console', (m) => console.log(`[${user.login}] ${m.type()}: ${m.text()}`));
    page.on('pageerror', (e) => console.log(`[${user.login}] pageerror: ${e.message}`));
    page.on('request', (r) => r.method() === 'PATCH' && console.log(`[${user.login}] PATCH`, JSON.stringify(r.headers())));
    page.on('response', (r) => r.url().includes('/collab/') && console.log(`[${user.login}] ${r.status()} ${r.request().method()} ${r.url().replace(/^.*\/admin/, '')}`));
  }
  await signIn(page, user.login, user.password);
  await page.goto(`/admin/items/${itemId}`);
  await expect(form(page).getByLabel('Title')).toBeVisible();
  await expect(form(page)).toHaveAttribute('data-collab', 'live', { timeout: 20_000 });
  return { ctx, page, user };
}

/** Two editors on one item, both joined and seeing each other; closed again when `fn` ends. */
export async function withPair(browser: Browser, a: Editor, b: Editor, itemId: string, fn: (A: Session, B: Session) => Promise<void>) {
  const A = await openSession(browser, a, itemId);
  const B = await openSession(browser, b, itemId);
  try {
    await expect(avatarOf(A.page, b.name)).toBeVisible({ timeout: 15_000 });
    await expect(avatarOf(B.page, a.name)).toBeVisible({ timeout: 15_000 });
    await fn(A, B);
  } finally {
    await A.ctx.close().catch(() => undefined);
    await B.ctx.close().catch(() => undefined);
  }
}

export const form = (page: Page) => page.getByRole('form', { name: /fields/ });

// ---- Selectors that depend on the presence UI (lib/collab/PresenceAvatars.svelte, marks.ts, messages/collab.ts) ----

/** The header group listing the other participants (labelled "Also here"; each avatar is named by its person). */
export const alsoHere = (page: Page) => page.getByRole('group', { name: 'Also here' });

/** The visually hidden live region announcing joins, leaves and who started editing which field. */
export const announcements = (page: Page) => page.getByTestId('collab-live');

/** The avatar of a participant in the group: an image named by their full display name (plus " (idle)" / " (viewing)"). */
export const avatarOf = (page: Page, name: string) => alsoHere(page).getByRole('img', { name }).first();

/** The field row another participant has focused carries the tag as `data-collab-name` ("Name is editing"; drawn by CSS). */
export const fieldTag = (page: Page, fieldPath: string, name: string) =>
  page.locator(`.h-item__form [data-field="${fieldPath}"][data-collab-name*="${name}"]`).first();

/** The "Unsaved changes" indicator next to the item meta line. */
export const unsaved = (page: Page) => page.getByRole('status').filter({ hasText: 'Unsaved changes' });

/** The toast shown when the draft changed outside the session and the form reloaded ("The draft was changed elsewhere. Reloaded the latest version."). */
export const resetNotice = (page: Page) => page.getByRole('region', { name: /notifications/i }).getByText(/Reloaded the latest version/).first();

/** The conflict dialog of a 412 on save. */
export const conflictDialog = (page: Page) => page.getByRole('dialog').filter({ hasText: /conflict|changed by someone|newer version/i });

export const saveButton = (page: Page) => page.getByRole('button', { name: 'Save', exact: true });

// ---- Form helpers ----

/** Types at the start or the end of a text input without replacing its content. */
export async function typeAt(field: Locator, where: 'start' | 'end', text: string) {
  // A part another client is still seeding keeps its fields read-only: typing into it would be lost.
  await expect(field).toBeEditable();
  await field.focus();
  // Not Control+Home/End: those keys move the caret differently per platform (macOS Chromium ignores them in inputs).
  await field.evaluate((el, w) => {
    const i = el as HTMLInputElement | HTMLTextAreaElement;
    const at = w === 'start' ? 0 : i.value.length;
    i.setSelectionRange(at, at);
  }, where);
  await field.pressSequentially(text, { delay: 20 });
}

/** Puts the caret at the start or the end of a rich text editor (Control+Home/End differ per platform). */
export async function caretIn(editor: Locator, where: 'start' | 'end') {
  await expect(editor).toBeEditable();
  await editor.focus();
  await editor.evaluate((el, w) => {
    const sel = window.getSelection()!;
    sel.selectAllChildren(el);
    if (w === 'start') sel.collapseToStart();
    else sel.collapseToEnd();
  }, where);
}

/**
 * Adds a list item (a "block" of the e2e schema's sections field) and returns its heading input.
 *
 * A peer may add its own item at the same time, and on a slow machine its item can reach this page before a
 * count check runs. So the new input is identified inside the page, in the same task as the click: the update is
 * flushed in microtasks, and a message from the peer cannot be handled in between.
 */
export async function addSection(page: Page, heading: string) {
  const mark = `e2e-mine-${Math.random().toString(36).slice(2)}`;
  const button = form(page).getByRole('button', { name: /Add item/ });
  await expect(button).toBeEnabled();
  const marked = await form(page).evaluate(async (formEl, m) => {
    const inputs = () => [...formEl.querySelectorAll('input')].filter((i) => i.labels?.[0]?.textContent?.trim() === 'Heading');
    const seen = new Set(inputs());
    const add = [...formEl.querySelectorAll('button')].find((b) => /Add item/.test(b.getAttribute('aria-label') ?? b.textContent ?? ''));
    add?.click();
    for (let i = 0; i < 5; i++) await new Promise<void>((r) => queueMicrotask(r));
    const fresh = inputs().filter((i) => !seen.has(i));
    if (fresh.length === 1) fresh[0].setAttribute('data-e2e-mine', m);
    return fresh.length;
  }, mark);
  expect(marked, 'exactly one new heading input right after the click').toBe(1);
  const input = form(page).locator(`[data-e2e-mine="${mark}"]`);
  await input.fill(heading);
  return input;
}

export const headingValues = (page: Page) => form(page).getByLabel('Heading').evaluateAll((els) => els.map((e) => (e as HTMLInputElement).value));
