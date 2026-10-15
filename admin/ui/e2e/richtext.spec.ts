import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { adminLogin, adminPassword, draftOf, manage, seedPage, seedRoots, signIn, tag, watchErrors } from './helpers';

const text = (s: string, marks?: unknown[]) => ({ type: 'text', text: s, ...(marks ? { marks } : {}) });

// The rich text editor on an article body: typing, shortcuts, input rules, the link popover with an item
// search, saving the stored document, "Edit as JSON", and axe.
test.describe.serial('rich text editor', () => {
  const t = tag();
  let target = { item: '', title: '' };
  let article = '';

  test.beforeAll(async () => {
    const roots = await seedRoots();
    target = { ...(await seedPage('careers', `Link target ${t}`, roots.careers.node)), title: `Link target ${t}` };
    const made = await manage('POST', '/collections/article/items', {
      locale: 'en',
      fields: {
        title: `Rich ${t}`,
        body: {
          type: 'doc',
          content: [
            { type: 'heading', attrs: { level: 2 }, content: [text('Welcome')] },
            { type: 'paragraph', content: [text('Plain and '), text('bold', [{ type: 'bold' }]), text(' words.')] },
            { type: 'bulletList', content: [{ type: 'listItem', content: [{ type: 'paragraph', content: [text('first point')] }] }] },
          ],
        },
      },
    });
    article = made.id;
  });

  test('shows the stored document, edits it and saves the stored JSON', async ({ page }) => {
    const errors = watchErrors(page);
    await signIn(page, adminLogin, adminPassword);
    await page.goto(`/admin/items/${article}`);
    const form = page.getByRole('form', { name: /fields/ });
    const editor = form.getByRole('textbox', { name: 'Body' });
    await expect(editor).toBeVisible();
    await expect(editor.getByRole('heading', { level: 2, name: 'Welcome' })).toBeVisible();
    await expect(editor.locator('strong')).toHaveText('bold');
    await expect(editor.getByRole('listitem')).toHaveText('first point');
    await expect(form.getByRole('toolbar', { name: 'Formatting' })).toBeVisible();

    // Input rules and shortcuts.
    await editor.getByRole('listitem').click();
    // ProseMirror reads the caret on the browser's selectionchange: wait for the toolbar to follow it.
    await expect(form.getByRole('button', { name: 'Bulleted list' })).toHaveAttribute('aria-pressed', 'true');
    await page.keyboard.press('End');
    await page.keyboard.press('Enter');
    await page.keyboard.press('Enter');
    await page.keyboard.type('### Third level');
    await page.keyboard.press('Enter');
    await page.keyboard.type('1. step one');
    await page.keyboard.press('Enter');
    await page.keyboard.type('step two');
    await page.keyboard.press('Enter');
    await page.keyboard.press('Enter');
    await page.keyboard.type('some ');
    await page.keyboard.press('ControlOrMeta+B');
    await page.keyboard.type('strong');
    await page.keyboard.press('ControlOrMeta+B');
    await page.keyboard.type(' text');
    await expect(editor.getByRole('heading', { level: 3, name: 'Third level' })).toBeVisible();
    await expect(editor.locator('ol > li')).toHaveCount(2);

    // A link to an item from the popover.
    for (const _ of 'some strong text') await page.keyboard.press('Shift+ArrowLeft');
    await expect.poll(() => page.evaluate(() => getSelection()!.toString())).toBe('some strong text');
    // ProseMirror takes the selection from the browser's (asynchronous) selectionchange event.
    await page.waitForTimeout(150);
    await page.keyboard.press('ControlOrMeta+K');
    const pop = page.getByRole('dialog', { name: 'Link' });
    await expect(pop).toBeVisible();
    await pop.getByRole('combobox', { name: 'Or link to an item' }).fill(`Link target ${t}`);
    await pop.getByRole('option', { name: new RegExp(`Link target ${t}`) }).click();
    await expect(editor.locator(`a[href="item:${target.item}"]`).first()).toBeVisible();

    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByText('Saved').first()).toBeVisible();
    const body = (await draftOf(article)).fields.body;
    expect(body.content.map((n: any) => n.type)).toEqual(['heading', 'paragraph', 'bulletList', 'heading', 'orderedList', 'paragraph']);
    expect(body.content[3]).toEqual({ type: 'heading', attrs: { level: 3 }, content: [text('Third level')] });
    const link = { type: 'link', attrs: { href: `item:${target.item}` } };
    expect(body.content[5].content).toEqual([text('some ', [link]), text('strong', [{ type: 'bold' }, link]), text(' text', [link])]);
    expect(errors).toEqual([]);
  });

  test('edits the document as JSON and rejects what the field does not hold', async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
    await page.goto(`/admin/items/${article}`);
    const form = page.getByRole('form', { name: /fields/ });
    await form.getByRole('button', { name: 'More' }).click();
    await page.getByRole('menuitem', { name: 'Edit as JSON' }).click();
    const dlg = page.getByRole('dialog', { name: /Edit Body as JSON/ });
    await dlg.getByRole('textbox').fill('{"type":"nope"}');
    await dlg.getByRole('button', { name: 'Apply' }).click();
    await expect(dlg.getByRole('alert')).toContainText('"type": "doc"');
    await dlg.getByRole('textbox').fill(JSON.stringify({ type: 'doc', content: [{ type: 'paragraph', content: [text('From JSON')] }] }));
    await dlg.getByRole('button', { name: 'Apply' }).click();
    await expect(dlg).toHaveCount(0);
    await expect(form.getByRole('textbox', { name: 'Body' })).toHaveText('From JSON');
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByText('Saved').first()).toBeVisible();
    expect((await draftOf(article)).fields.body).toEqual({ type: 'doc', content: [{ type: 'paragraph', content: [text('From JSON')] }] });
  });

  test('has no serious accessibility violations, with the link popover open too', async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
    await page.goto(`/admin/items/${article}`);
    const editor = page.getByRole('textbox', { name: 'Body' });
    await expect(editor).toBeVisible();
    const scan = async () => {
      const r = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'best-practice']).analyze();
      return r.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical').map((v) => `${v.id}: ${v.nodes.slice(0, 3).map((n) => n.target.join(' ')).join(' | ')}`);
    };
    expect(await scan()).toEqual([]);
    await page.getByRole('toolbar', { name: 'Formatting' }).getByRole('button', { name: 'Link' }).click();
    const pop = page.getByRole('dialog', { name: 'Link' });
    await expect(pop).toBeVisible();
    // The popover fades in; axe measures contrast on the settled colours.
    await pop.evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)));
    expect(await scan()).toEqual([]);
  });
});
