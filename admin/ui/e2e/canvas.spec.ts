import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Locator, type Page } from '@playwright/test';
import { add, card, clickBlock, frameOf, frameOrder, toolbar } from './blocks-helpers';
import { adminLogin, adminPassword, draftOf, manage, signIn, tag, watchErrors } from './helpers';

// Visual editing (H7) against examples/starter with the preview host in e2e/fixtures/preview-host: live mode on the
// en channel, reload mode on the de channel. The host marks blocks with data-h-path; titles and headings are mapped
// by their text.
const t = tag();
test.setTimeout(90_000);
test.use({ actionTimeout: 15_000 });

const hero = (heading: string) => ({ type: 'hero', props: { heading } });

async function openPreview(page: Page, id: string, status: 'Live' | 'Reload mode' = 'Live') {
  await signIn(page, adminLogin, adminPassword);
  await page.goto(`/admin/items/${id}`);
  await page.getByRole('button', { name: 'Preview', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: status })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Edit on canvas' })).toHaveAttribute('aria-pressed', 'true');
}

/** Double clicks a text in the frame, replaces it and commits with Enter. */
async function inlineEdit(page: Page, target: Locator, value: string) {
  await target.dblclick();
  await expect(frameOf(page).locator('[contenteditable]')).toBeVisible();
  await page.keyboard.press('ControlOrMeta+a');
  await page.keyboard.type(value);
  await page.keyboard.press('Enter');
  await expect(frameOf(page).locator('[contenteditable]')).toHaveCount(0);
}

const center = async (l: Locator) => {
  const b = (await l.boundingBox())!;
  return { x: b.x + b.width / 2, y: b.y + b.height / 2 };
};

async function violations(page: Page) {
  const result = await new AxeBuilder({ page }).exclude('iframe').withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'best-practice']).analyze();
  expect(result.passes.length, 'axe ran').toBeGreaterThan(10);
  return result.violations
    .filter((v) => v.impact === 'serious' || v.impact === 'critical')
    .map((v) => `${v.id} (${v.impact}): ${v.help}\n${v.nodes.slice(0, 3).map((n) => '    ' + n.target.join(' ')).join('\n')}`);
}

test.describe.serial('canvas editing, live mode', () => {
  let id = '';

  test.beforeAll(async () => {
    id = (await manage('POST', '/collections/landing/items', { locale: 'en', fields: { title: `Canvas ${t}`, body: [hero('Alpha'), hero('Beta')] } })).id;
  });

  test('clicking a block in the frame selects it and opens its card', async ({ page }) => {
    await openPreview(page, id);
    const frame = frameOf(page);
    await expect(frame.locator('[data-h-block="hero"]')).toHaveCount(2);
    await clickBlock(frame.locator('[data-h-block="hero"]').nth(1));
    await expect(page.locator('.h-canvas__sel')).toHaveAttribute('data-path', 'fields/body/1');
    await expect(toolbar(page)).toHaveAccessibleName(/^Block: Hero/);
    const heading = card(page, 'hero').nth(1).getByLabel(/^Heading/);
    await expect(heading).toBeVisible();
    await expect(heading).toBeInViewport();
    await expect(heading).toHaveValue('Beta');
  });

  test('double clicking a heading edits it in place; Enter updates the form and the frame', async ({ page }) => {
    await openPreview(page, id);
    const frame = frameOf(page);
    await inlineEdit(page, frame.locator('[data-h-block="hero"] .h').first(), `Alpha ${t}`);
    await expect(card(page, 'hero').first().getByLabel(/^Heading/)).toHaveValue(`Alpha ${t}`);
    await expect(frame.locator('[data-h-block="hero"] .h').first()).toHaveText(`Alpha ${t}`);
    // The title is edited the same way (a field, no block).
    await inlineEdit(page, frame.locator('#title'), `Canvas ${t} edited`);
    await expect(page.getByRole('textbox', { name: 'Title' })).toHaveValue(`Canvas ${t} edited`);
    await expect(frame.locator('#title')).toHaveText(`Canvas ${t} edited`);
  });

  test('axe is clean with the canvas on and a selection', async ({ page }) => {
    await openPreview(page, id);
    const frame = frameOf(page);
    await clickBlock(frame.locator('[data-h-block="hero"]').first());
    await expect(toolbar(page)).toBeVisible();
    expect(await violations(page)).toEqual([]);
    // Dark: reload so no colour transition is half-way when axe measures.
    await page.emulateMedia({ colorScheme: 'dark' });
    await page.reload();
    await page.getByRole('button', { name: 'Preview', exact: true }).click();
    await expect(page.getByRole('status').filter({ hasText: 'Live' })).toBeVisible();
    await clickBlock(frame.locator('[data-h-block="hero"]').first());
    await expect(toolbar(page)).toBeVisible();
    expect(await violations(page)).toEqual([]);
  });
});

test('a page is built entirely on the canvas', async ({ page }) => {
  const errors = watchErrors(page);
  const made = (await manage('POST', '/collections/landing/items', { locale: 'en', fields: { title: `Exit ${t}` } })).id;
  await openPreview(page, made);
  const frame = frameOf(page);
  const blocks = frame.locator('#blocks > [data-h-block]');

  // Add a hero to the empty body from the canvas.
  await page.getByRole('button', { name: 'Add block to Body' }).click();
  await page.getByRole('option', { name: /^Hero/ }).click();
  await expect.poll(() => frameOrder(page)).toEqual(['hero']);
  // A new hero has an empty heading, which has no element to edit: give it a first value in the form, then edit it in place.
  await card(page, 'hero').getByLabel(/^Heading/).fill('Hero one');
  await expect(frame.locator('[data-h-block="hero"] .h')).toHaveText('Hero one');
  await inlineEdit(page, frame.locator('[data-h-block="hero"] .h'), `Hero ${t}`);
  await expect(card(page, 'hero').getByLabel(/^Heading/)).toHaveValue(`Hero ${t}`);

  // Insert after the hero: a Cards block.
  await clickBlock(blocks.first());
  await toolbar(page).getByRole('button', { name: 'Insert after' }).click();
  await page.getByRole('option', { name: /^Cards/ }).click();
  await expect.poll(() => frameOrder(page)).toEqual(['hero', 'cards']);

  // Add a card inside it.
  await clickBlock(frame.locator('#blocks > [data-h-block="cards"]'));
  await toolbar(page).getByRole('button', { name: 'Add inside' }).click();
  await page.getByRole('menuitem').first().click();
  await page.getByRole('option', { name: /^Card/ }).click();
  await expect(frame.locator('[data-h-block="card"]')).toHaveCount(1);
  await card(page, 'card').getByLabel(/^Title/).fill('Solo');
  await expect(frame.locator('[data-h-block="card"] .h')).toHaveText('Solo');

  // Alt+Up moves the selected cards block above the hero.
  await clickBlock(frame.locator('#blocks > [data-h-block="cards"]'));
  await toolbar(page).getByRole('button', { name: 'Move up' }).focus();
  await page.keyboard.press('Alt+ArrowUp');
  await expect.poll(() => frameOrder(page)).toEqual(['cards', 'hero']);

  // Drag the hero's handle onto the top of the cards block.
  await clickBlock(frame.locator('#blocks > [data-h-block="hero"]'));
  await expect(page.locator('.h-canvas__sel')).toHaveAttribute('data-path', 'fields/body/1');
  await expect(toolbar(page)).toHaveAccessibleName(/^Block: Hero/);
  const handle = page.locator('.h-canvas__handle');
  const from = await center(handle);
  const cardsBox = (await frame.locator('#blocks > [data-h-block="cards"]').boundingBox())!;
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  await page.mouse.move(from.x, from.y - 12, { steps: 3 });
  await page.mouse.move(cardsBox.x + cardsBox.width / 2, cardsBox.y + 3, { steps: 8 });
  await expect(page.locator('.h-canvas__drop')).toHaveCount(1);
  await page.mouse.up();
  await expect.poll(() => frameOrder(page)).toEqual(['hero', 'cards']);

  // A card cannot leave its slot: not allowed at the root, so no drop indicator and nothing moves.
  await clickBlock(frame.locator('[data-h-block="card"]'));
  await expect(page.locator('.h-canvas__sel')).toHaveAttribute('data-path', 'fields/body/1/slots/items/0');
  await expect(toolbar(page)).toHaveAccessibleName(/^Block: Card/);
  const cardHandle = await center(page.locator('.h-canvas__handle'));
  const heroBox = (await blocks.first().boundingBox())!;
  await page.mouse.move(cardHandle.x, cardHandle.y);
  await page.mouse.down();
  await page.mouse.move(cardHandle.x, cardHandle.y - 12, { steps: 3 });
  await page.mouse.move(heroBox.x + heroBox.width / 2, heroBox.y + heroBox.height / 2, { steps: 8 });
  await expect(page.locator('.h-canvas__drop')).toHaveCount(0);
  await page.mouse.up();
  await expect.poll(() => frameOrder(page)).toEqual(['hero', 'cards']);
  await expect(frame.locator('#blocks > [data-h-block="cards"] [data-h-block="card"]')).toHaveCount(1);

  // Duplicate the hero; delete one and undo.
  await clickBlock(blocks.first());
  await toolbar(page).getByRole('button', { name: 'Duplicate' }).click();
  await expect.poll(() => frameOrder(page)).toEqual(['hero', 'hero', 'cards']);
  await clickBlock(blocks.first());
  await toolbar(page).getByRole('button', { name: 'Delete' }).click();
  await expect.poll(() => frameOrder(page)).toEqual(['hero', 'cards']);
  await page.locator('.h-toast__action').click();
  await expect.poll(() => frameOrder(page)).toEqual(['hero', 'hero', 'cards']);

  await page.keyboard.press('Control+s');
  await expect(page.getByRole('button', { name: 'Saved' })).toBeVisible();
  const draft = await draftOf(made);
  const body = draft.fields.body as any[];
  expect(body.map((b) => b.type)).toEqual(['hero', 'hero', 'cards']);
  expect(body[0].props.heading).toBe(`Hero ${t}`);
  expect(body[1].props.heading).toBe(`Hero ${t}`);
  expect(body[2].slots.items.map((c: any) => c.props.title)).toEqual(['Solo']);
  expect(new Set(body.map((b) => b.id)).size).toBe(3);
  expect(errors).toEqual([]);
});

test.describe.serial('canvas editing, reload mode', () => {
  let id = '';

  test.beforeAll(async () => {
    id = (await manage('POST', '/collections/landing/items', { locale: 'de', fields: { title: `Leinwand ${t}`, body: [hero('Eins'), hero('Zwei')] } })).id;
  });

  test('click selects, inline edit commits, and the reloaded frame shows it with the selection back', async ({ page }) => {
    await openPreview(page, id, 'Reload mode');
    const frame = frameOf(page);
    await expect(frame.locator('[data-h-block="hero"]')).toHaveCount(2);
    await clickBlock(frame.locator('[data-h-block="hero"]').nth(1));
    await expect(page.locator('.h-canvas__sel')).toHaveAttribute('data-path', 'fields/body/1');
    await expect(card(page, 'hero').nth(1).getByLabel(/^Heading/)).toBeVisible();

    await inlineEdit(page, frame.locator('[data-h-block="hero"] .h').nth(1), `Zwei ${t}`);
    await expect(card(page, 'hero').nth(1).getByLabel(/^Heading/)).toHaveValue(`Zwei ${t}`);
    // The relay, then the frame reloads with the new text; nothing is saved.
    await expect(frame.locator('[data-h-block="hero"] .h').nth(1)).toHaveText(`Zwei ${t}`);
    // The double click selected the heading itself; after the reload it is mapped again.
    await expect(page.locator('.h-canvas__sel')).toHaveAttribute('data-path', 'fields/body/1/props/heading');
    const draft = await draftOf(id, 'de');
    expect(draft.fields.body[1].props.heading).toBe('Zwei');
  });
});
