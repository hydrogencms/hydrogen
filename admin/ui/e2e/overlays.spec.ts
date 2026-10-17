import { expect, test } from '@playwright/test';
import { adminLogin, adminPassword, draftOf, manage, signIn, watchErrors } from './helpers';
import { axeViolations } from './publishing-helpers';
import { bodyText, expectDeCH, fieldRow, publishEditor, publishedGermanArticle, richText, saveEditor } from './overlay-helpers';

// H8d: de-CH inherits de (examples/multisite registers the overlays module). Setup and delivery assertions go
// through the management and delivery APIs; the overlay actions go through the admin.
// Toasts fade in: axe measures colours mid-animation otherwise.
const settled = (page: import('@playwright/test').Page) =>
  page.waitForFunction(() => [...document.querySelectorAll('.h-toast')].every((t) => getComputedStyle(t).opacity === '1'));

test.describe('locale overlays', () => {
  test('de-CH inherits de, overrides the title, follows de and resets', async ({ page }) => {
    const errors = watchErrors(page);
    const failed: string[] = [];
    page.on('response', (r) => r.status() >= 400 && r.status() !== 401 && failed.push(new URL(r.url()).pathname));
    await signIn(page, adminLogin, adminPassword);
    const a = await publishedGermanArticle('Overlay');

    // Derived availability: no de-CH document exists, yet the channel serves de content as de-CH.
    await expectDeCH(a.id, (i) => i && i.locale === 'de-CH' && i.fields.title === a.title && bodyText(i.fields.body) === 'Deutscher Text');
    const view = await manage('GET', `/items/${a.id}?locale=de-CH`);
    expect(view.inherits).toBe('de');
    expect(view.overrides ?? []).toEqual([]);
    expect(view.inheritedFrom.title).toBe('de');
    expect(view.availableLocales).toContain('de-CH');
    expect(view.locales).not.toContain('de-CH');

    await page.goto(`/admin/items/${a.id}?locale=de-CH`);
    const title = fieldRow(page, 'Title');
    await expect(title.getByText('Inherited from de')).toBeVisible();
    await expect(title.getByLabel(/^Title/)).toBeDisabled();
    await expect(title.getByLabel(/^Title/)).toHaveValue(a.title);
    await expect(fieldRow(page, 'Body').getByText('Inherited from de')).toBeVisible();
    await settled(page);
    expect(await axeViolations(page)).toEqual([]);

    // Override the title: save stores only the title, publish serves it with the inherited rest.
    await title.getByRole('button', { name: 'Override', exact: true }).click();
    await expect(title.getByText('Overridden')).toBeVisible();
    const override = `${a.title} CH`;
    await title.getByLabel(/^Title/).fill(override);
    await saveEditor(page);
    const draft = await draftOf(a.id, 'de-CH');
    expect(draft.overrides).toEqual(['title']);
    expect(draft.fields.title).toBe(override);
    expect(draft.inheritedFrom.body).toBe('de');
    await publishEditor(page);
    await expectDeCH(a.id, (i) => i && i.locale === 'de-CH' && i.fields.title === override && bodyText(i.fields.body) === 'Deutscher Text');
    await settled(page);
    expect(await axeViolations(page)).toEqual([]);

    // A new de body reaches de-CH without touching de-CH.
    await manage('PATCH', `/items/${a.id}`, { locale: 'de', fields: { body: richText('Neuer Text') } });
    await manage('POST', `/items/${a.id}/publish`, { locales: ['de'] });
    await expectDeCH(a.id, (i) => i && i.fields.title === override && bodyText(i.fields.body) === 'Neuer Text');
    expect((await manage('GET', `/items/${a.id}?locale=de-CH`)).overrides).toEqual(['title']);

    // Reset to de: the save sends null, the override goes, de-CH follows de again after publishing.
    await page.goto(`/admin/items/${a.id}?locale=de-CH`);
    await expect(fieldRow(page, 'Title').getByText('Overridden')).toBeVisible();
    await fieldRow(page, 'Title').getByRole('button', { name: 'Reset to de', exact: true }).click();
    await expect(fieldRow(page, 'Title').getByText('Inherited from de')).toBeVisible();
    await saveEditor(page);
    expect((await draftOf(a.id, 'de-CH')).overrides ?? []).toEqual([]);
    await publishEditor(page);
    await expectDeCH(a.id, (i) => i && i.locale === 'de-CH' && i.fields.title === a.title && bodyText(i.fields.body) === 'Neuer Text');
    // Publishing resets the collaboration session; a presence heartbeat already in flight then gets 404 and the
    // client re-joins (the protocol), and the browser logs that 404. Any other failed request still fails the test.
    expect(errors.filter((e) => !/status of 404/.test(e))).toEqual([]);
    expect(failed.filter((u) => !/\/collab\/items\/[^/]+\/presence$/.test(u))).toEqual([]);
  });
});
