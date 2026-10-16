import { expect, test } from '@playwright/test';
import { adminLogin, adminPassword, manage, signIn, signOut, tag, watchErrors } from './helpers';
import { axeViolations, draftArticle, expectDelivered } from './publishing-helpers';
import { asToken, chip, editorToken, editorUser, expectReview, openTab, publishButton } from './review-helpers';

// H8b review workflow on `article` (review: true in the e2e schema). The submitter and the approver are two editor
// users; the principals the other specs use (management token, administrator) hold review.bypass.
test.describe.serial('review workflow', () => {
  let submitter: Awaited<ReturnType<typeof editorUser>>;
  let approver: Awaited<ReturnType<typeof editorUser>>;
  let tokenS = '';
  let article: { id: string; title: string };

  test.beforeAll(async () => {
    submitter = await editorUser('submitter');
    approver = await editorUser('approver');
    tokenS = await editorToken('submitter');
    article = await draftArticle('Review flow');
  });

  test('1. the submitter sends the draft to review; Publish stays disabled with the reason', async ({ page }) => {
    const errors = watchErrors(page);
    await signIn(page, submitter.login, submitter.password);
    await page.goto(`/admin/items/${article.id}`);
    await expect(publishButton(page)).toBeDisabled();
    await expect(publishButton(page)).toHaveAccessibleDescription('Needs an approved review');

    await page.getByRole('button', { name: 'Submit for review…' }).click();
    const dlg = page.getByRole('dialog', { name: 'Submit for review' });
    await dlg.getByLabel('Comment').fill('Please check the headline.');
    await dlg.getByRole('button', { name: 'Submit for review' }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(chip(page, 'In review')).toBeVisible();
    await expect(publishButton(page)).toBeDisabled();

    const review = await manage('GET', `/items/${article.id}/review`);
    expect(review.state).toBe('in_review');
    expect(review.required).toBe(true);
    expect(review.events.map((e: any) => e.kind ?? e.type)).toContain('submitted');
    expect(JSON.stringify(review.events)).toContain('Please check the headline.');

    await openTab(page, 'Review');
    await expect(page.getByText('Please check the headline.')).toBeVisible();
    expect(errors).toEqual([]);
  });

  test('2. four eyes: the submitter cannot approve their own submission', async ({ page }) => {
    await signIn(page, submitter.login, submitter.password);
    await page.goto(`/admin/items/${article.id}`);
    await expect(chip(page, 'In review')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Approve', exact: true })).toHaveCount(0);

    // Through the API as a non-admin principal that submitted and then tries to approve.
    const own = await draftArticle('Four eyes');
    expect((await asToken(tokenS, 'POST', `/items/${own.id}/review/submit`, { comment: 'mine' })).status).toBe(200);
    const res = await asToken(tokenS, 'POST', `/items/${own.id}/review/approve`, {});
    expect(res.status).toBe(403);
    expect(res.code).toBe('four_eyes');
    await expectReview(own.id, 'in_review');
  });

  test('3. a reviewer requests changes with a comment; the submitter resubmits', async ({ page }) => {
    await signIn(page, approver.login, approver.password);
    await page.goto(`/admin/items/${article.id}`);
    await expect(chip(page, 'In review')).toBeVisible();
    await page.getByRole('button', { name: 'Request changes…' }).click();
    const dlg = page.getByRole('dialog', { name: 'Request changes' });
    await dlg.getByLabel('Comment').fill('Headline is too long.');
    await dlg.getByRole('button', { name: 'Request changes' }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(chip(page, 'Changes requested')).toBeVisible();
    await openTab(page, 'Review');
    await expect(page.getByText('Headline is too long.')).toBeVisible();
    await expectReview(article.id, 'changes_requested');
    await expect(publishButton(page)).toBeDisabled();

    await signOut(page);
    await signIn(page, submitter.login, submitter.password);
    await page.goto(`/admin/items/${article.id}`);
    await expect(chip(page, 'Changes requested')).toBeVisible();
    await page.getByRole('button', { name: 'Submit for review…' }).click();
    await page.getByRole('dialog', { name: 'Submit for review' }).getByRole('button', { name: 'Submit for review' }).click();
    await expect(chip(page, 'In review')).toBeVisible();
    await openTab(page, 'Review');
    await expect(page.getByText('Headline is too long.')).toBeVisible();
  });

  test('4. approval, an edit reopens the review, approval again and publish', async ({ page }) => {
    await signIn(page, approver.login, approver.password);
    await page.goto(`/admin/items/${article.id}`);
    await page.getByRole('button', { name: 'Approve', exact: true }).click();
    await expect(chip(page, 'Approved')).toBeVisible();
    await expectReview(article.id, 'approved');
    await expect(publishButton(page)).toBeEnabled();

    // Any draft change after approval puts the item back in review.
    await manage('PATCH', `/items/${article.id}`, { locale: 'en', fields: { title: `${article.title} edited` } });
    await expectReview(article.id, 'in_review');
    const review = await manage('GET', `/items/${article.id}/review`);
    expect(review.events.map((e: any) => e.kind ?? e.type)).toContain('reopened');
    await page.reload();
    await expect(chip(page, 'In review')).toBeVisible();
    await expect(publishButton(page)).toBeDisabled();

    await page.getByRole('button', { name: 'Approve', exact: true }).click();
    await expect(chip(page, 'Approved')).toBeVisible();
    await publishButton(page).click();
    const dlg = page.getByRole('dialog', { name: 'Publish' });
    await dlg.getByRole('button', { name: 'Publish', exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expectDelivered(article.id, 200);
    // Publishing closes the review.
    await expectReview(article.id, 'none');
  });

  test('5. the Reviews queue lists in-review items and those assigned to me', async ({ page }) => {
    const errors = watchErrors(page);
    const mine = await draftArticle('Queue assigned');
    const other = await draftArticle('Queue unassigned');
    const sub = await asToken(tokenS, 'POST', `/items/${mine.id}/review/submit`, { reviewers: [`user:${approver.id}`] });
    expect(sub.status).toBe(200);
    expect((await asToken(tokenS, 'POST', `/items/${other.id}/review/submit`, {})).status).toBe(200);

    await signIn(page, approver.login, approver.password);
    await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Reviews' }).click();
    await expect(page).toHaveURL(/\/admin\/reviews/);
    await page.getByRole('tab', { name: 'Assigned to me' }).click();
    await expect(page.getByRole('row').filter({ hasText: mine.title })).toBeVisible();
    await expect(page.getByRole('row').filter({ hasText: other.title })).toHaveCount(0);
    await page.getByRole('tab', { name: 'In review' }).click();
    await expect(page.getByRole('row').filter({ hasText: mine.title })).toBeVisible();
    await expect(page.getByRole('row').filter({ hasText: other.title })).toBeVisible();
    // Reviewer names come with the rows: an editor needs no access to the user list.
    await expect(page.getByRole('row').filter({ hasText: mine.title })).toContainText(approver.name);
    await page.getByRole('row').filter({ hasText: other.title }).getByRole('link').first().click();
    await expect(page).toHaveURL(new RegExp(`/admin/items/${other.id}`));

    await page.goto('/admin/reviews');
    await page.getByRole('tab', { name: 'In review' }).click();
    await expect(page.getByRole('row').filter({ hasText: mine.title })).toBeVisible();
    expect(await axeViolations(page)).toEqual([]);
    expect(errors).toEqual([]);
  });

  test('6. publishing without approval is refused: 409 review_required, also from a release', async () => {
    const a = await draftArticle('Needs review');
    let res = await asToken(tokenS, 'POST', `/items/${a.id}/publish`, {});
    expect(res.status).toBe(409);
    expect(res.code).toBe('review_required');
    // Submitted but not approved is still refused.
    expect((await asToken(tokenS, 'POST', `/items/${a.id}/review/submit`, {})).status).toBe(200);
    res = await asToken(tokenS, 'POST', `/items/${a.id}/publish`, {});
    expect(res.status).toBe(409);
    expect(res.code).toBe('review_required');

    // A release with the unapproved item fails per item with the same code; it publishes nothing.
    const rel = await asToken(tokenS, 'POST', '/releases', { name: `Review release ${tag()}` });
    expect(rel.status).toBeLessThan(300);
    const rid = rel.json.id;
    expect((await asToken(tokenS, 'PUT', `/releases/${rid}/items/${a.id}`, { action: 'publish' })).status).toBeLessThan(300);
    await asToken(tokenS, 'POST', `/releases/${rid}/publish`, {});
    const failed = await manage('GET', `/releases/${rid}`);
    expect(failed.status).toBe('failed');
    expect(failed.errors).toHaveLength(1);
    expect(failed.errors[0].item).toBe(a.id);
    expect(failed.errors[0].code).toBe('review_required');
    await expectDelivered(a.id, 404);
  });

  test('7. an administrator sees "Publish without review" and the audit records the bypass', async ({ page }) => {
    const a = await draftArticle('Bypass');
    await asToken(tokenS, 'POST', `/items/${a.id}/review/submit`, {});
    await signIn(page, adminLogin, adminPassword);
    await page.goto(`/admin/items/${a.id}`);
    await expect(chip(page, 'In review')).toBeVisible();
    await page.getByRole('button', { name: 'Publish options' }).click();
    await page.getByRole('menuitem', { name: 'Publish without review' }).click();
    const dlg = page.getByRole('dialog');
    await dlg.getByRole('button', { name: 'Publish without review' }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expectDelivered(a.id, 200);

    const audit = await manage('GET', `/audit?subject=${encodeURIComponent(a.id)}&limit=50`);
    expect(JSON.stringify(audit)).toContain('reviewBypassed');
  });

  test('8. withdrawing a review returns it to none', async ({ page }) => {
    const a = await draftArticle('Withdraw');
    await signIn(page, submitter.login, submitter.password);
    await page.goto(`/admin/items/${a.id}`);
    await page.getByRole('button', { name: 'Submit for review…' }).click();
    await page.getByRole('dialog', { name: 'Submit for review' }).getByRole('button', { name: 'Submit for review' }).click();
    await expect(chip(page, 'In review')).toBeVisible();
    await page.getByRole('button', { name: 'Withdraw review' }).click();
    await expectReview(a.id, 'none');
    await expect(page.getByRole('button', { name: 'Submit for review…' })).toBeVisible();
  });
});
