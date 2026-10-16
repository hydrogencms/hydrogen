import { expect, test } from '@playwright/test';
import { draftOf, manage } from './helpers';
import { editorUser } from './review-helpers';
import {
  addSection, alsoHere, caretIn, announcements, avatarOf, collabArticle, collabItem, conflictDialog, fieldTag, form, headingValues,
  openSession, resetNotice, saveButton, typeAt, unsaved, withPair, type Editor,
} from './collab-helpers';

// H8c collaboration: two editors in two browser contexts on one item. Presence, soft locks, Yjs convergence,
// save/saved and external resets. Every test sets up its own item and sessions, so one failure does not cascade.
// The "blocks" of the multisite e2e schema are the items of the `sections` list of groups.
test.describe('collaboration', () => {
  let alex: Editor;
  let blake: Editor;

  test.beforeAll(async () => {
    alex = await editorUser('alex');
    blake = await editorUser('blake');
  });

  test('each sees the other\'s avatar, and the join is announced', async ({ browser }) => {
    const item = await collabItem('Presence');
    const A = await openSession(browser, alex, item.id);
    const B = await openSession(browser, blake, item.id);
    try {
      await expect(alsoHere(A.page)).toBeVisible({ timeout: 15_000 });
      await expect(avatarOf(A.page, blake.name)).toBeVisible();
      await expect(avatarOf(B.page, alex.name)).toBeVisible({ timeout: 15_000 });
      await expect(announcements(A.page)).toContainText(`${blake.name} joined`);
    } finally {
      await A.ctx.close();
      await B.ctx.close();
    }
  });

  test('focusing a field shows the other\'s outline and name tag, and describes the field', async ({ browser }) => {
    const item = await collabItem('Focus', { intro: 'middle' });
    await withPair(browser, alex, blake, item.id, async (A, B) => {
      await form(A.page).getByLabel('Intro').focus();
      await expect(fieldTag(B.page, 'intro', alex.name)).toBeVisible();
      const desc = await form(B.page).getByLabel('Intro').getAttribute('aria-describedby');
      expect(desc).toBeTruthy();
      await expect(B.page.locator(`#${desc!.split(' ').at(-1)}`)).toHaveText(new RegExp(`${alex.name} is editing`));
      await expect(announcements(B.page)).toContainText(`${alex.name} is editing`);
      await form(A.page).getByLabel('Title').focus();
      await expect(fieldTag(B.page, 'intro', alex.name)).toHaveCount(0);
      await expect(fieldTag(B.page, 'title', alex.name)).toBeVisible();
    });
  });

  test('typing in different fields converges, and concurrent typing in one field keeps both insertions', async ({ browser }) => {
    const item = await collabItem('Typing', { intro: 'middle' });
    await withPair(browser, alex, blake, item.id, async (A, B) => {
      const [aTitle, bTitle] = [form(A.page).getByLabel('Title'), form(B.page).getByLabel('Title')];
      const [aIntro, bIntro] = [form(A.page).getByLabel('Intro'), form(B.page).getByLabel('Intro')];
      await Promise.all([typeAt(aTitle, 'end', ' by A'), typeAt(bIntro, 'end', ' by B')]);
      await expect(bTitle).toHaveValue(`${item.title} by A`);
      await expect(aIntro).toHaveValue('middle by B');
      await Promise.all([typeAt(aIntro, 'start', 'AAA '), typeAt(bIntro, 'end', ' BBB')]);
      const want = 'AAA middle by B BBB';
      await expect(aIntro).toHaveValue(want);
      await expect(bIntro).toHaveValue(want);
    });
  });

  test('adding a block in both converges: both present, same order', async ({ browser }) => {
    const item = await collabItem('Blocks');
    await withPair(browser, alex, blake, item.id, async (A, B) => {
      await Promise.all([addSection(A.page, 'From A'), addSection(B.page, 'From B')]);
      await expect.poll(() => headingValues(A.page)).toHaveLength(2);
      // Wait for both headings' text, not just two items: typing reaches the other side a little later.
      await expect.poll(async () => [...(await headingValues(A.page))].sort(), { timeout: 15_000 }).toEqual(['From A', 'From B']);
      const a = await headingValues(A.page);
      await expect.poll(() => headingValues(B.page), { timeout: 15_000 }).toEqual(a);
    });
  });

  test('rich text co-editing: both insertions survive in both editors', async ({ browser }) => {
    const item = await collabArticle('Rich', 'middle');
    await withPair(browser, alex, blake, item.id, async (A, B) => {
      const [aBody, bBody] = [form(A.page).getByRole('textbox', { name: 'Body' }), form(B.page).getByRole('textbox', { name: 'Body' })];
      await expect(aBody).toContainText('middle');
      await expect(bBody).toContainText('middle');
      await caretIn(aBody, 'end');
      await caretIn(bBody, 'start');
      await Promise.all([A.page.keyboard.type(' by A', { delay: 20 }), B.page.keyboard.type('B: ', { delay: 20 })]);
      await expect(aBody).toContainText('B: middle by A');
      await expect(bBody).toContainText('B: middle by A');
      await saveButton(A.page).click();
      await expect(A.page.getByText('Saved').first()).toBeVisible();
      const saved = await draftOf(item.id);
      expect(JSON.stringify(saved.fields.body)).toContain('B: middle by A');
    });
  });

  test('A saves: B is clean and its next edit saves without a conflict', async ({ browser }) => {
    const item = await collabItem('Save', { intro: 'middle' });
    await withPair(browser, alex, blake, item.id, async (A, B) => {
      await Promise.all([typeAt(form(A.page).getByLabel('Title'), 'end', ' by A'), typeAt(form(B.page).getByLabel('Intro'), 'end', ' by B')]);
      await expect(form(B.page).getByLabel('Title')).toHaveValue(`${item.title} by A`);
      await expect(form(A.page).getByLabel('Intro')).toHaveValue('middle by B');
      await expect(unsaved(A.page)).toBeVisible();
      await saveButton(A.page).click();
      await expect(A.page.getByText('Saved').first()).toBeVisible();
      await expect(unsaved(B.page)).toHaveCount(0);
      const saved = await draftOf(item.id);
      expect(saved.fields.intro).toBe('middle by B');

      await typeAt(form(B.page).getByLabel('Title'), 'end', ' +B');
      await expect(unsaved(B.page)).toBeVisible();
      await saveButton(B.page).click();
      await expect(B.page.getByText('Saved').first()).toBeVisible();
      await expect(conflictDialog(B.page)).toHaveCount(0);
      expect((await draftOf(item.id)).fields.title).toBe(`${item.title} by A +B`);
      await expect(unsaved(A.page)).toHaveCount(0);
    });
  });

  test('a save that lacks B\'s unsent edit leaves B dirty, and B\'s save then keeps both', async ({ browser }) => {
    const item = await collabItem('Unsynced', { intro: 'middle' });
    await withPair(browser, alex, blake, item.id, async (A, B) => {
      // B's updates cannot reach the server for a while: its typing stays unsynced.
      await B.page.route('**/collab/items/*/updates', (r) => r.abort());
      await typeAt(form(B.page).getByLabel('Intro'), 'end', ' by B');
      await expect(unsaved(B.page)).toBeVisible();
      await typeAt(form(A.page).getByLabel('Title'), 'end', ' by A');
      await saveButton(A.page).click();
      await expect(A.page.getByText('Saved').first()).toBeVisible();
      await expect(form(B.page).getByLabel('Title')).toHaveValue(`${item.title} by A`);
      // A's save does not hold B's edit: B stays dirty (with the new ETag).
      await expect(unsaved(B.page)).toBeVisible();
      await B.page.unroute('**/collab/items/*/updates');
      await expect(form(A.page).getByLabel('Intro')).toHaveValue('middle by B', { timeout: 20_000 });
      await saveButton(B.page).click();
      await expect(B.page.getByText('Saved').first()).toBeVisible();
      await expect(conflictDialog(B.page)).toHaveCount(0);
      const saved = await draftOf(item.id);
      expect(saved.fields.title).toBe(`${item.title} by A`);
      expect(saved.fields.intro).toBe('middle by B');
    });
  });

  test('an API write to the draft resets both forms to the API value', async ({ browser }) => {
    const item = await collabItem('Reset');
    await withPair(browser, alex, blake, item.id, async (A, B) => {
      const title = `API ${item.title}`;
      await manage('PATCH', `/items/${item.id}`, { locale: 'en', fields: { title } });
      for (const s of [A, B]) {
        await expect(resetNotice(s.page)).toBeVisible({ timeout: 20_000 });
        await expect(form(s.page).getByLabel('Title')).toHaveValue(title);
        await expect(form(s.page)).toHaveAttribute('data-collab', 'live', { timeout: 20_000 });
      }
      // The session works again after the reload.
      await typeAt(form(A.page).getByLabel('Title'), 'end', '!');
      await expect(form(B.page).getByLabel('Title')).toHaveValue(`${title}!`);
    });
  });

  test('closing B\'s page (with its unload handlers) removes its avatar from A', async ({ browser }) => {
    const item = await collabItem('Leave');
    const A = await openSession(browser, alex, item.id);
    const B = await openSession(browser, blake, item.id);
    try {
      await expect(avatarOf(A.page, blake.name)).toBeVisible({ timeout: 15_000 });
      B.page.on('dialog', (d) => void d.accept());
      await B.page.close({ runBeforeUnload: true });
      // Leave on pagehide: far sooner than the 45 s a silent client lives.
      await expect(avatarOf(A.page, blake.name)).toHaveCount(0, { timeout: 15_000 });
      await expect(announcements(A.page)).toContainText(`${blake.name} left`);
    } finally {
      await A.ctx.close();
      await B.ctx.close();
    }
  });
});
