import { expect, test } from '@playwright/test';
import { card, expand } from './blocks-helpers';
import { adminLogin, adminPassword, manage, signIn, tag } from './helpers';

// A document written through the management API is not "unsaved" when the editor opens it, and opening it writes
// no draft: a review approval hashes the draft, so any write would make it stale. Runs against examples/starter.
const doc = {
  type: 'doc',
  content: [
    { type: 'heading', attrs: { level: 2 }, content: [{ type: 'text', text: 'Heading' }] },
    { type: 'paragraph', content: [{ type: 'text', text: 'Intro', marks: [{ type: 'bold' }] }] },
    { type: 'bulletList', content: [{ type: 'listItem', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'item' }] }] }] },
    { type: 'paragraph' },
  ],
};

test('opening an item written through the API shows no unsaved changes and writes nothing', async ({ page }) => {
  const t = tag();
  const created = await manage('POST', '/collections/landing/items', {
    locale: 'en',
    fields: {
      title: `Loaded ${t}`,
      body: [
        { type: 'hero', props: { heading: 'Welcome' } },
        { type: 'richText', props: { body: doc } },
        { type: 'cards', slots: { items: [{ type: 'card', props: { title: 'One' } }] } },
        { type: 'callout', props: { text: 'Note' } },
      ],
    },
  });
  await signIn(page, adminLogin, adminPassword);
  const writes: string[] = [];
  page.on('request', (r) => {
    const u = new URL(r.url());
    if (u.pathname.startsWith('/api/manage/v1') && !['GET', 'HEAD'].includes(r.method()) && !u.pathname.includes('/collab/')) writes.push(`${r.method()} ${u.pathname}`);
  });
  await page.goto(`/admin/items/${created.id}`);
  await expect(page.getByRole('textbox', { name: 'Title' })).toHaveValue(`Loaded ${t}`);
  for (const type of ['hero', 'richText', 'cards', 'callout']) await expand(card(page, type));
  await expect(page.locator('.h-rte__doc')).toBeVisible();
  await page.waitForTimeout(2000);
  await expect(page.getByText('Unsaved changes')).toHaveCount(0);
  expect(writes).toEqual([]);

  await page.getByRole('textbox', { name: 'Title' }).fill(`Loaded ${t} edited`);
  await expect(page.getByText('Unsaved changes').first()).toBeVisible();
});
