import { expect, test, type Page } from '@playwright/test';
import { pick, adminLogin, adminPassword, careersURL, draftOf, manage, openSettings, route, seedPage, seedRoots, signIn, signOut, tag, watchErrors } from './helpers';

// The H5b exit scenario as one ordered story: each test builds on the previous one, so the describe is serial.
test.describe.serial('exit scenario: an editor confined to the careers site', () => {
  const t = tag();
  const editor = { login: `editor-${t}`, password: `Pw-${t}-correct-horse-1` };
  const roots = {} as Awaited<ReturnType<typeof seedRoots>>;
  let corporate: { item: string; node: string; title: string };
  const jobs = { title: `Jobs ${t}`, slug: `jobs-${t}`, id: '' };
  const target = { title: `Teams ${t}`, slug: `teams-${t}`, item: '', node: '' };

  test.beforeAll(async () => {
    Object.assign(roots, await seedRoots());
    corporate = await seedPage('corporate', `Corporate secret ${t}`, roots.corporate.node);
    const tg = await seedPage('careers', target.title, roots.careers.node);
    target.item = tg.item;
    target.node = tg.node;
  });

  test('1. the administrator creates the editor, confines them to careers and makes a token', async ({ page }) => {
    const errors = watchErrors(page);
    await signIn(page, adminLogin, adminPassword);

    await openSettings(page);
    await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Users' }).click();
    await page.getByRole('button', { name: 'New' }).click();
    const dlg = page.getByRole('dialog', { name: 'New user' });
    await dlg.getByLabel('Login').fill(editor.login);
    await dlg.getByLabel('Name').fill(`E2E Editor ${t}`);
    await dlg.getByLabel('Password').fill(editor.password);
    await dlg.getByRole('button', { name: 'Create' }).click();
    await page.getByRole('link', { name: `E2E Editor ${t}` }).click();

    await page.getByRole('button', { name: 'Add role' }).click();
    const assign = page.getByRole('dialog', { name: 'Add role' });
    await pick(page, assign, 'Role', 'editor');
    // Limitations the role supports: the channel and the pages site, both ticked from the catalog.
    await assign.getByRole('group', { name: 'channel' }).getByLabel('careers_en').check();
    await assign.getByRole('group', { name: 'pages.site' }).getByLabel('careers', { exact: true }).check();
    await assign.getByRole('button', { name: 'Add role' }).click();
    const row = page.getByRole('row', { name: /editor/ });
    await expect(row).toContainText('channel: careers_en');
    await expect(row).toContainText('pages.site: careers');

    // An API token: the secret appears once, in the dialog that created it.
    await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'API tokens' }).click();
    await page.getByRole('button', { name: 'New' }).click();
    const tok = page.getByRole('dialog', { name: 'New token' });
    await tok.getByLabel('Name').fill(`token-${t}`);
    await tok.getByRole('group', { name: 'Roles' }).getByLabel('viewer').check();
    await tok.getByRole('button', { name: 'Create' }).click();
    const secret = (await page.getByTestId('secret').textContent())!.trim();
    expect(secret).toMatch(/^hyt_/);
    await page.getByRole('dialog').getByRole('button', { name: 'Close' }).first().click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(page.getByRole('row', { name: new RegExp(`token-${t}`) })).toBeVisible();
    await expect(page.getByTestId('secret')).toHaveCount(0);
    await expect(page.locator('body')).not.toContainText(secret);
    await page.reload();
    await expect(page.getByRole('row', { name: new RegExp(`token-${t}`) })).toBeVisible();
    await expect(page.locator('body')).not.toContainText(secret);
    const list = await manage('GET', '/tokens?limit=200');
    expect(JSON.stringify(list)).not.toContain(secret);

    await signOut(page);
    expect(errors).toEqual([]);
  });

  const nav = (page: Page) => page.getByRole('navigation', { name: 'Main' });

  test('2a. the editor sees only careers collections and the careers site', async ({ page }) => {
    const errors = watchErrors(page);
    await signIn(page, editor.login, editor.password);
    await expect(nav(page).getByRole('link', { name: 'Page', exact: true })).toBeVisible();
    await expect(nav(page).getByRole('link', { name: 'Landing' })).toBeVisible();
    for (const hidden of ['Article', 'Product', 'Redirect', 'corporate']) {
      await expect(nav(page).getByRole('link', { name: hidden, exact: true })).toHaveCount(0);
    }
    await expect(nav(page).getByRole('link', { name: 'careers', exact: true })).toBeVisible();
    await expect(nav(page).getByRole('link', { name: 'Users' })).toHaveCount(0);

    // The corporate page list is empty or refused, and never shows corporate items.
    await page.goto('/admin/collections/article');
    await expect(page.getByText(corporate.title)).toHaveCount(0);
    await page.goto('/admin/pages/corporate');
    await expect(page.getByText('Corporate Home')).toHaveCount(0);
    await expect(page.getByText(corporate.title)).toHaveCount(0);
    expect(errors.filter((e) => !/40[34]/.test(e))).toEqual([]);
  });

  test('2b. the editor adds a child page and fills its fields', async ({ page }) => {
    await signIn(page, editor.login, editor.password);
    await nav(page).getByRole('link', { name: 'careers', exact: true }).click();
    const tree = page.getByRole('tree', { name: /careers/ });
    await expect(tree.getByRole('treeitem', { name: /Careers Home/ })).toBeVisible();
    await tree.getByRole('treeitem', { name: /Careers Home/ }).click();
    // The row re-renders while its languages load, which can swallow the first click on the menu button.
    await expect(async () => {
      await page.getByRole('button', { name: 'Actions for Careers Home' }).click();
      await expect(page.getByRole('menuitem', { name: 'Add child page' })).toBeVisible({ timeout: 1000 });
    }).toPass();
    await page.getByRole('menuitem', { name: 'Add child page' }).click();
    const dlg = page.getByRole('dialog', { name: 'Add child page' });
    await pick(page, dlg, 'Type', 'Page');
    await dlg.getByLabel('Name').fill(jobs.title);
    await dlg.getByRole('button', { name: 'Create' }).click();

    const form = page.getByRole('form', { name: /fields/ });
    await expect(form.getByLabel('Title')).toHaveValue(jobs.title);
    await form.getByLabel('Intro').fill('Work with us');
    await expect(form.getByLabel('Slug', { exact: true })).toHaveValue(jobs.slug);
    await pick(page, form, 'Kind', 'team');
    await form.getByRole('combobox', { name: /Related/ }).fill('Teams');
    await page.getByRole('option', { name: new RegExp(target.title) }).click();
    await form.getByRole('button', { name: /Add item/ }).click();
    await form.getByLabel('Heading').fill('Why join');
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByText('Saved').first()).toBeVisible();

    const found = await manage('GET', `/collections/page/items?q=${encodeURIComponent(jobs.title)}`);
    jobs.id = found.items[0].id;
    const item = await draftOf(jobs.id);
    expect(item.fields).toMatchObject({ title: jobs.title, slug: jobs.slug, kind: 'team', intro: 'Work with us', related: target.item });
    expect(item.fields.sections).toEqual([expect.objectContaining({ heading: 'Why join' })]);
  });

  test('2c. the editor translates, publishes both locales and sees the live URL', async ({ page }) => {
    await signIn(page, editor.login, editor.password);
    await page.goto(`/admin/items/${jobs.id}`);
    await expect(page.getByRole('heading', { name: jobs.title, level: 1 })).toBeVisible();

    await page.getByRole('button', { name: 'de', exact: true }).click();
    await expect(page.getByRole('button', { name: 'de', exact: true })).toHaveAttribute('aria-pressed', 'true');
    await page.getByRole('button', { name: 'Copy from en' }).click();
    const form = page.getByRole('form', { name: /fields/ });
    await expect(form.getByLabel('Title')).toHaveValue(jobs.title);
    await form.getByLabel('Title').fill(`Stellen ${t}`);
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByText('Saved').first()).toBeVisible();
    expect((await draftOf(jobs.id, 'de')).fields.title).toBe(`Stellen ${t}`);

    await page.getByRole('button', { name: 'Publish', exact: true }).click();
    const dlg = page.getByRole('dialog', { name: 'Publish' });
    await expect(dlg.getByRole('checkbox', { name: /^en/ })).toBeChecked();
    await expect(dlg.getByRole('checkbox', { name: /^de$/ })).toBeChecked();
    await dlg.getByRole('button', { name: 'Publish', exact: true }).click();
    await expect(page.getByText('Published', { exact: true }).first()).toBeVisible();
    const live = await manage('GET', `/items/${jobs.id}`);
    expect(live.locales).toEqual(expect.arrayContaining(['en', 'de']));

    // "Where this appears": the careers channel with its live address.
    await page.getByRole('tab', { name: 'Appears' }).click();
    const appears = page.getByRole('tabpanel', { name: 'Appears' });
    await expect(appears.getByText('careers_en')).toBeVisible();
    for (const locale of ['en', 'de']) {
      const entry = appears.getByRole('listitem').filter({ hasText: `https://jobs.acme.com/${jobs.slug}` }).filter({ hasText: `${locale} \u00b7 careers` });
      await expect(entry.getByText('live')).toBeVisible();
    }
    const delivered = await route('careers_en', careersURL(`/${jobs.slug}`));
    expect(delivered.located.kind).toBe('page');
    expect(delivered.item.id).toBe(jobs.id);
  });

  test('2d. corporate content is invisible to the editor', async ({ page }) => {
    await signIn(page, editor.login, editor.password);
    await page.goto(`/admin/items/${corporate.item}`);
    await expect(page.getByRole('heading', { name: 'Not found' })).toBeVisible();
    await expect(page.getByText(corporate.title)).toHaveCount(0);

    await page.goto('/admin/collections/page');
    await expect(page.getByRole('link', { name: jobs.title })).toBeVisible();
    await expect(page.getByText(corporate.title)).toHaveCount(0);
    await page.getByRole('searchbox', { name: 'Search', exact: true }).fill('Corporate secret');
    await expect(page.getByText('No item matches the search and filters.')).toBeVisible();

    // The corporate collections themselves are not offered or are empty.
    await page.goto('/admin/collections/article');
    await expect(page.getByText(/Nothing here yet|No .* items yet|not delivered by this channel|No item matches/).first()).toBeVisible();
    await page.goto('/admin/pages/corporate');
    await expect(page.getByText('Corporate Home')).toHaveCount(0);
  });

  test('3. the editor moves the page with the keyboard and the old URL redirects', async ({ page }) => {
    const before = careersURL(`/${jobs.slug}`);
    const kids = await manage('GET', `/pages/sites/careers/tree?parent=${roots.careers.node}&depth=1`);
    const node = kids.items.find((n: any) => n.item === jobs.id).id as string;
    await signIn(page, editor.login, editor.password);
    await page.goto(`/admin/pages/careers?node=${node}`);
    const tree = page.getByRole('tree', { name: /careers/ });
    const row = tree.getByRole('treeitem', { name: new RegExp(jobs.title) });
    await expect(row).toBeVisible();
    await row.focus();
    await page.keyboard.press('m');
    const dlg = page.getByRole('dialog', { name: `Move “${jobs.title}”` });
    await expect(dlg).toBeVisible();
    await dlg.getByRole('tree', { name: 'New parent' }).getByRole('treeitem', { name: new RegExp(target.title) }).click();
    const changes = dlg.getByRole('region').or(dlg.locator('section')).filter({ hasText: 'URLs that change' });
    await expect(changes).toContainText(`/${jobs.slug}`);
    await expect(changes).toContainText(`/${target.slug}/${jobs.slug}`);
    await expect(changes).toContainText('redirected');
    await dlg.getByRole('button', { name: 'Move', exact: true }).click();
    await expect(dlg).toHaveCount(0);

    // The old address answers with a redirect to the new one (delivery caches are purged asynchronously).
    await expect
      .poll(async () => (await route('careers_en', before)).located.kind, { timeout: 15_000 })
      .toBe('redirect');
    const moved = await route('careers_en', before);
    expect(JSON.stringify(moved)).toContain(`/${target.slug}/${jobs.slug}`);
    const now = await route('careers_en', careersURL(`/${target.slug}/${jobs.slug}`));
    expect(now.located.kind).toBe('page');
    expect(now.item.id).toBe(jobs.id);
  });

  test('4. a trash blocked by a reference shows the blocker', async ({ page }) => {
    const referrer = await seedPage('careers', `Referrer ${t}`, roots.careers.node, { related: jobs.id });
    await signIn(page, editor.login, editor.password);
    await page.goto(`/admin/items/${jobs.id}`);
    await page.getByRole('button', { name: 'More actions' }).click();
    await page.getByRole('menuitem', { name: 'Move to trash' }).click();
    const dlg = page.getByRole('dialog');
    await dlg.getByRole('button', { name: 'Move to trash' }).click();
    await expect(dlg.getByRole('alert')).toContainText('Referenced by 1 items');
    await expect(dlg.getByRole('link', { name: referrer.title })).toBeVisible();
    await expect(dlg.getByRole('button', { name: 'Move to trash' })).toBeDisabled();
    await dlg.getByRole('button', { name: 'Cancel' }).click();
    expect((await manage('GET', `/items/${jobs.id}`)).id).toBe(jobs.id);
  });
});
