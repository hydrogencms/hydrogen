import { expect, test } from '@playwright/test';
import { adminLogin, adminPassword, draftOf, manage, signIn, tag, watchErrors } from './helpers';

const text = (s: string) => ({ type: 'text', text: s });

// Custom rich text nodes and the alignment control, against examples/content (E2E_PROJECT=content in
// scripts/admin-e2e.sh): its schema declares callout, details, embed and a highlight mark.
test.describe.serial('rich text: custom nodes (examples/content)', () => {
  const t = tag();
  let article = '';

  test.beforeAll(async () => {
    const made = await manage('POST', '/collections/article/items', {
      locale: 'en',
      fields: { title: `Custom rich ${t}`, body: { type: 'doc', content: [{ type: 'paragraph', content: [text('Aligned text')] }] } },
    });
    article = made.id;
  });

  test('inserts a callout, aligns a paragraph and saves the stored document', async ({ page }) => {
    const errors = watchErrors(page);
    await signIn(page, adminLogin, adminPassword);
    await page.goto(`/admin/items/${article}`);
    const form = page.getByRole('form', { name: /fields/ });
    const editor = form.getByRole('textbox', { name: 'Body' });
    const bar = form.getByRole('toolbar', { name: 'Formatting' });
    await expect(editor.getByText('Aligned text')).toBeVisible();

    await editor.getByText('Aligned text').click();
    await bar.getByRole('button', { name: 'Insert' }).click();
    await page.getByRole('menuitem', { name: 'Callout' }).click();
    const callout = editor.locator('aside.rt-callout');
    await expect(callout).toBeVisible();
    await page.keyboard.type('Heads up');
    await expect(callout).toHaveText(/Heads up/);

    // Edit the callout's attributes from the toolbar.
    await bar.getByRole('button', { name: 'Edit Callout' }).click();
    const dlg = page.getByRole('dialog', { name: 'Edit Callout' });
    await dlg.getByLabel('variant').selectOption('warning');
    await dlg.getByRole('button', { name: 'Apply' }).click();
    await expect(callout).toHaveAttribute('data-variant', 'warning');

    await editor.getByText('Aligned text').click();
    await bar.getByRole('button', { name: 'Alignment and options' }).click();
    await page.getByRole('menuitemradio', { name: 'Align center' }).click();
    await expect(editor.locator('p[data-align=center]')).toHaveText('Aligned text');

    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByText('Saved').first()).toBeVisible();
    const body = (await draftOf(article)).fields.body;
    expect(body.content).toEqual([
      { type: 'paragraph', attrs: { align: 'center' }, content: [text('Aligned text')] },
      { type: 'callout', attrs: { variant: 'warning' }, content: [{ type: 'paragraph', content: [text('Heads up')] }] },
    ]);
    expect(errors).toEqual([]);
  });
});
