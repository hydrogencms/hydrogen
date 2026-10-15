import { expect, type Page } from '@playwright/test';

export const base = process.env.BASE_URL ?? 'http://127.0.0.1:8080';
export const adminLogin = process.env.ADMIN_LOGIN ?? 'admin';
export const adminPassword = process.env.ADMIN_PASSWORD ?? 'e2e-admin-password-1';
const manageToken = process.env.MANAGE_TOKEN ?? 'dev-token';

/** A lower-case, URL-safe suffix so reruns on the same database never collide. */
export const tag = () => Math.random().toString(36).slice(2, 8);

/** The management API with the configured bearer token (loopback only). */
export async function manage<T = any>(method: string, path: string, body?: unknown, expect_: number[] = [200, 201, 204]): Promise<T> {
  const res = await fetch(`${base}/api/manage/v1${path}`, {
    method,
    headers: { Authorization: `Bearer ${manageToken}`, ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  if (!expect_.includes(res.status)) throw new Error(`${method} ${path}: ${res.status} ${text}`);
  return (text ? JSON.parse(text) : undefined) as T;
}

/** The draft view of an item in one locale (what the editor shows). */
export const draftOf = (id: string, locale = 'en') => manage('GET', `/items/${id}?draft=true&locale=${locale}`);

/** The public delivery API. */
export async function deliver<T = any>(path: string): Promise<T> {
  const res = await fetch(`${base}/api/deliver/v1${path}`);
  if (!res.ok) throw new Error(`deliver ${path}: ${res.status}`);
  return (await res.json()) as T;
}

export const careersURL = (path: string) => `https://jobs.acme.com${path}`;
export const route = (channel: string, url: string) => deliver(`/route?channel=${channel}&url=${encodeURIComponent(url)}`);

/** Creates a published page item and places it in a site's tree (a root when `parent` is empty). */
export async function seedPage(site: string, title: string, parentNode = '', extra: Record<string, unknown> = {}) {
  const item = await manage('POST', '/collections/page/items', { locale: 'en', fields: { title, ...extra } });
  await manage('POST', `/items/${item.id}/publish`, {});
  const node = await manage('POST', `/pages/sites/${site}/nodes`, { kind: 'page', item: item.id, ...(parentNode ? { parent: parentNode } : {}) });
  return { item: item.id as string, node: node.id as string, title };
}

/** Both sites need a root page before anything can be added under them; idempotent. */
export async function seedRoots() {
  const out: Record<string, { item: string; node: string }> = {};
  for (const [site, name, url] of [
    ['careers', 'Careers Home', 'https://jobs.acme.com/'],
    ['corporate', 'Corporate Home', 'https://www.acme.com/en'],
  ] as const) {
    const channel = site === 'careers' ? 'careers_en' : 'corporate_en';
    const r = await route(channel, url);
    if (r.located?.kind === 'page') out[site] = { item: r.item.id, node: r.located.resolution.node };
    else out[site] = await seedPage(site, name);
  }
  return out;
}

export async function signIn(page: Page, login: string, password: string) {
  await page.goto('/admin/login');
  await page.getByLabel('Login').fill(login);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('button', { name: /^Account menu/ })).toBeVisible();
}

export async function signOut(page: Page) {
  await page.getByRole('button', { name: /^Account menu/ }).click();
  await page.getByRole('menuitem', { name: 'Sign out' }).click();
  await expect(page).toHaveURL(/\/admin\/login/);
}

/** Collects console errors, ignoring the signed-out session probe's 401. */
export function watchErrors(page: Page) {
  const errors: string[] = [];
  page.on('console', (m) => m.type() === 'error' && !m.text().includes('401') && errors.push(m.text()));
  page.on('pageerror', (e) => errors.push(String(e)));
  return errors;
}
