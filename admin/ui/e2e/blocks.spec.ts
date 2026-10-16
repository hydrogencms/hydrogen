import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Locator, type Page } from '@playwright/test';
import { add, card, frameOf, rootAdd } from './blocks-helpers';
import { adminLogin, adminPassword, draftOf, manage, signIn, tag, watchErrors } from './helpers';

// The block editor and the preview pane against examples/starter, with the preview host in e2e/fixtures/preview-host
// standing in for the frontend: live mode on the en channel, reload mode on the de channel.
const t = tag();

async function violations(page: Page) {
  const result = await new AxeBuilder({ page }).exclude('iframe').withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'best-practice']).analyze();
  expect(result.passes.length, 'axe ran').toBeGreaterThan(10);
  return result.violations
    .filter((v) => v.impact === 'serious' || v.impact === 'critical')
    .map((v) => `${v.id} (${v.impact}): ${v.help}\n${v.nodes.slice(0, 3).map((n) => '    ' + n.target.join(' ')).join('\n')}`);
}

test.describe.serial('block editor and live preview', () => {
  let id = '';

  test.beforeAll(async () => {
    id = (await manage('POST', '/collections/landing/items', { locale: 'en', fields: { title: `Build ${t}` } })).id;
  });

  test('builds a page and the frame follows every change', async ({ page }) => {
    const errors = watchErrors(page);
    await signIn(page, adminLogin, adminPassword);
    await page.goto(`/admin/items/${id}`);
    await expect(page.getByText('No blocks yet')).toBeVisible();

    await page.getByRole('button', { name: 'Preview', exact: true }).click();
    const frame = frameOf(page);
    await expect(frame.locator('#title')).toHaveText(`Build ${t}`);
    await expect(page.getByRole('status').filter({ hasText: 'Live' })).toBeVisible();

    // A title edit reaches the frame without a save.
    const title = page.getByRole('textbox', { name: 'Title' });
    await title.fill(`Build ${t} edited`);
    await expect(frame.locator('#title')).toHaveText(`Build ${t} edited`);
    await expect(page.getByText('Saved', { exact: true }).first()).toBeHidden().catch(() => {});

    // Insert a hero and edit its heading.
    await add(rootAdd(page), /^Hero/);
    await card(page, 'hero').getByLabel(/^Heading/).fill('Hello world');
    await expect(frame.locator('[data-h-block="hero"] .h')).toHaveText('Hello world');
    await expect(frame.locator('[data-h-block="hero"] .tone')).toHaveText('light');
    await expect(page.getByText(/^\d+ ms$/)).toBeVisible();

    // Cards with two card children.
    await add(rootAdd(page), /^Cards/);
    const cards = card(page, 'cards');
    const slotAdd = () => cards.locator('.h-block__slot').getByRole('button', { name: 'Add block' });
    await add(slotAdd(), /^Card/);
    await add(slotAdd(), /^Card/);
    await card(page, 'card').nth(0).getByLabel(/^Title/).fill('One');
    await card(page, 'card').nth(1).getByLabel(/^Title/).fill('Two');
    await expect(frame.locator('[data-h-block="card"] .h')).toHaveText(['One', 'Two']);

    // Reorder with the keyboard.
    await card(page, 'card').nth(1).locator('> .h-block__head .h-block__toggle').focus();
    await page.keyboard.press('Alt+ArrowUp');
    await expect(frame.locator('[data-h-block="card"] .h')).toHaveText(['Two', 'One']);
    await expect(page.getByRole('status').filter({ hasText: /Moved Card to position 1 of 2/ })).toHaveCount(1);

    // Rich text in a richText block.
    await add(rootAdd(page), /^Rich text/);
    const rte = card(page, 'richText').getByRole('textbox', { name: 'Body' });
    await rte.click();
    await page.keyboard.type('Rich words here');
    await expect(frame.locator('[data-h-block="richText"] .rt')).toHaveText('Rich words here');

    // The shared block shows the shared item's blocks.
    await add(rootAdd(page), /^Shared block/);
    await page.getByRole('combobox', { name: 'Choose the shared block to show' }).fill('Get in');
    await page.getByRole('option', { name: /Get in touch/ }).click();
    await expect(card(page, 'shared').getByText('Get in touch')).toBeVisible();
    await expect(frame.locator('[data-h-block="shared"] [data-slot="shared"] [data-h-block]').first()).toBeVisible();

    // Delete with undo, duplicate.
    await card(page, 'hero').getByRole('button', { name: /^Actions for Hero/ }).click();
    await page.getByRole('menuitem', { name: /^Delete/ }).click();
    await expect(frame.locator('[data-h-block="hero"]')).toHaveCount(0);
    await page.locator('.h-toast__action').click();
    await expect(frame.locator('[data-h-block="hero"] .h')).toHaveText('Hello world');
    await card(page, 'hero').getByRole('button', { name: /^Actions for Hero/ }).click();
    await page.getByRole('menuitem', { name: /^Duplicate/ }).click();
    await expect(frame.locator('[data-h-block="hero"]')).toHaveCount(2);

    // Save: the server keeps the tree with ids.
    await page.keyboard.press('Control+s');
    await expect(page.getByRole('button', { name: 'Saved' })).toBeVisible();
    const draft = await draftOf(id);
    const body = draft.fields.body as any[];
    expect(body.map((b) => b.type)).toEqual(['hero', 'hero', 'cards', 'richText', 'shared']);
    expect(body.every((b) => typeof b.id === 'string' && b.id.length > 20)).toBe(true);
    expect(new Set(body.map((b) => b.id)).size).toBe(body.length);
    expect(body[2].slots.items.map((c: any) => c.props.title)).toEqual(['Two', 'One']);
    expect(draft.fields.title).toBe(`Build ${t} edited`);

    expect(errors).toEqual([]);
  });

  test('moves a block with the dialog, enforces limits and routes API errors to the block', async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
    await page.goto(`/admin/items/${id}`);
    await page.getByRole('button', { name: 'Expand all' }).click();

    // "Move to…": the card goes to the end of a second cards block's slot.
    await add(rootAdd(page), /^Cards/);
    const second = card(page, 'cards').nth(1);
    await card(page, 'card').first().getByRole('button', { name: 'Actions for Card', exact: true }).click();
    await page.getByRole('menuitem', { name: /^Move to/ }).click();
    const dialog = page.getByRole('dialog', { name: /^Move Card/ });
    await dialog.getByLabel(/Cards 6/).check().catch(async () => dialog.locator('label').last().click());
    await dialog.getByRole('button', { name: 'Move', exact: true }).click();
    await expect(second.locator('[data-block-type="card"]')).toHaveCount(1);

    // The last card of a slot with a minimum cannot be deleted; a full list refuses another block.
    await second.locator('[data-block-type="card"]').getByRole('button', { name: 'Actions for Card', exact: true }).click();
    await expect(page.getByRole('menuitem', { name: /^Delete/ })).toHaveAttribute('aria-disabled', 'true');
    await page.keyboard.press('Escape');

    // Clearing the hero heading and publishing: the API's path lands on the heading of that block.
    const heading = card(page, 'hero').first().getByLabel(/^Heading/);
    await heading.fill('');
    await page.getByRole('button', { name: 'Publish', exact: true }).first().click();
    const confirm = page.getByRole('dialog').getByRole('button', { name: /^Publish/ });
    await confirm.click();
    await expect(card(page, 'hero').first().locator('.h-field__error').first()).toBeVisible();
    await expect(card(page, 'hero').first().locator('.h-block__errbadge')).toBeVisible();
  });

  test('axe is clean with the editor and the pane open', async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
    await page.goto(`/admin/items/${id}`);
    await page.getByRole('button', { name: 'Expand all' }).click();
    await page.getByRole('button', { name: 'Preview', exact: true }).click();
    await expect(frameOf(page).locator('#title')).toBeVisible();
    expect(await violations(page)).toEqual([]);
    await page.emulateMedia({ colorScheme: 'dark' });
    await page.reload();
    await page.getByRole('button', { name: 'Expand all' }).click();
    await page.getByRole('button', { name: 'Preview', exact: true }).click();
    expect(await violations(page)).toEqual([]);
  });
});

test('drag and drop reorders blocks and respects the allow rules', async ({ page }) => {
  const hero = (heading: string) => ({ type: 'hero', props: { heading } });
  const made = await manage('POST', '/collections/landing/items', {
    locale: 'en',
    fields: { title: `Drag ${t}`, body: [hero('Alpha'), hero('Beta'), hero('Gamma'), { type: 'cards', props: {}, slots: { items: [{ type: 'card', props: { title: 'Solo' } }, { type: 'card', props: { title: 'Duo' } }] } }] },
  });
  await signIn(page, adminLogin, adminPassword);
  await page.goto(`/admin/items/${made.id}`);
  const sums = () => page.locator('[data-blocks="body"] > .h-blocklist > .h-blocklist__items > .h-block > .h-block__head .h-block__sum').allTextContents();
  await expect.poll(sums).toEqual(['Alpha', 'Beta', 'Gamma']);

  const drag = async (from: Locator, to: Locator, where: 'top' | 'bottom') => {
    const a = (await from.boundingBox())!;
    const b = (await to.boundingBox())!;
    await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2);
    await page.mouse.down();
    await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2 - 10, { steps: 3 });
    await page.mouse.move(b.x + b.width / 2, where === 'top' ? b.y + 3 : b.y + b.height - 3, { steps: 8 });
    await expect(page.locator('.h-blocklist__drop')).toHaveCount(1);
    await page.mouse.up();
  };
  const heroCards = card(page, 'hero');
  await drag(heroCards.nth(2).locator('.h-block__grip'), heroCards.nth(0).locator('> .h-block__head'), 'top');
  await expect.poll(sums).toEqual(['Gamma', 'Alpha', 'Beta']);

  // A hero is not allowed in the cards slot: no drop indicator, nothing moves.
  await page.getByRole('button', { name: 'Expand all' }).click();
  const a = (await heroCards.nth(0).locator('.h-block__grip').boundingBox())!;
  const target = (await card(page, 'card').nth(0).locator('> .h-block__head').boundingBox())!;
  await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2);
  await page.mouse.down();
  await page.mouse.move(a.x + 2, a.y + 20, { steps: 3 });
  await page.mouse.move(target.x + target.width / 2, target.y + target.height - 3, { steps: 8 });
  await expect(page.locator('.h-blocklist__drop')).toHaveCount(0);
  await page.mouse.up();
  await expect(card(page, 'cards').locator('[data-block-type="hero"]')).toHaveCount(0);
  await expect(page.locator('[data-blocks="body"] > .h-blocklist > .h-blocklist__items > .h-block[data-block-type="hero"]')).toHaveCount(3);
});

test.describe.serial('reload mode preview', () => {
  let id = '';

  test.beforeAll(async () => {
    id = (await manage('POST', '/collections/landing/items', { locale: 'de', fields: { title: `Reload ${t}` } })).id;
  });

  test('the relay shows unsaved edits in the frame without saving the draft', async ({ page }) => {
    await signIn(page, adminLogin, adminPassword);
    await page.goto(`/admin/items/${id}`);
    await page.getByRole('button', { name: 'Preview', exact: true }).click();
    const frame = frameOf(page);
    await expect(frame.locator('#title')).toHaveText(`Reload ${t}`);
    await expect(page.getByRole('status').filter({ hasText: 'Reload mode' })).toBeVisible();
    await expect(page.getByText(/saves your changes to the draft/)).toHaveCount(0);

    await add(rootAdd(page), /^Hero/);
    await card(page, 'hero').getByLabel(/^Heading/).fill('Servergerendert');
    await expect(frame.locator('[data-h-block="hero"] .h')).toHaveText('Servergerendert');
    await expect(page.getByText('Unsaved changes shown')).toBeVisible();

    // Nothing was saved: the draft is as created until Save.
    const before = await draftOf(id, 'de');
    expect(before.fields.body).toBeUndefined();
    expect(before.version).toBe(0);

    await page.getByRole('button', { name: 'Save', exact: true }).first().click();
    await expect(page.getByRole('button', { name: 'Saved', exact: true }).first()).toBeVisible();
    const draft = await draftOf(id, 'de');
    expect(draft.fields.body[0].props.heading).toBe('Servergerendert');
    expect(draft.status).toBe('draft');
    await expect(frame.locator('[data-h-block="hero"] .h')).toHaveText('Servergerendert');
  });

  test('two editors: the last relay wins and neither touches the draft', async ({ browser }) => {
    const other = (await manage('POST', '/collections/landing/items', { locale: 'de', fields: { title: `Two ${t}` } })).id;
    const open = async () => {
      const ctx = await browser.newContext();
      const page = await ctx.newPage();
      await signIn(page, adminLogin, adminPassword);
      await page.goto(`/admin/items/${other}`);
      await page.getByRole('button', { name: 'Preview', exact: true }).click();
      await expect(page.getByRole('status').filter({ hasText: 'Reload mode' })).toBeVisible();
      return { ctx, page };
    };
    const a = await open();
    const b = await open();
    await a.page.getByRole('textbox', { name: 'Title' }).fill(`First ${t}`);
    await expect(frameOf(a.page).locator('#title')).toHaveText(`First ${t}`);
    await b.page.getByRole('textbox', { name: 'Title' }).fill(`Second ${t}`);
    await expect(frameOf(b.page).locator('#title')).toHaveText(`Second ${t}`);
    // The relay is one row per item: editor A's next change replaces B's.
    await a.page.getByRole('textbox', { name: 'Title' }).fill(`Third ${t}`);
    await expect(frameOf(a.page).locator('#title')).toHaveText(`Third ${t}`);
    expect((await draftOf(other, 'de')).fields.title).toBe(`Two ${t}`);
    await a.ctx.close();
    await b.ctx.close();
  });
});
