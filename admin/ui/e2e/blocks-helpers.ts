import type { Locator, Page } from '@playwright/test';

/** The preview iframe of the item editor. */
export const frameOf = (page: Page) => page.frameLocator('iframe[title^="Preview of"]');

/** A block card of the editor, by block type. */
export const card = (page: Page, type: string) => page.locator(`[data-block-type="${type}"]`);

/** The "Add block" button below the root list of the body field. */
export const rootAdd = (page: Page) => page.locator('[data-blocks="body"] > .h-blocklist > .h-blocklist__foot').getByRole('button', { name: 'Add block' });

/** Opens a palette from its trigger and picks a block type. */
export async function add(trigger: Locator, name: RegExp | string) {
  await trigger.click();
  await trigger.page().getByRole('option', { name }).click();
}

/** Expands a collapsed block card. */
export const expand = (c: Locator) => c.locator('> .h-block__head .h-block__toggle').click();
