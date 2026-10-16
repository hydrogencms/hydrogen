import { expect, type Page } from '@playwright/test';
import { base, manage, tag } from './helpers';

/** A user holding the built-in editor role (content.edit, review.submit, review.approve) and an API token with that role. */
export async function editorUser(label: string) {
  const t = tag();
  const login = `${label}-${t}`;
  const password = `Pw-${t}-correct-horse-1`;
  const user = await manage('POST', '/users', { login, name: `E2E ${label} ${t}`, password });
  const roles = await manage('GET', '/roles?limit=200');
  const role = roles.items.find((r: { name: string }) => r.name === 'editor');
  await manage('POST', '/assignments', { roleId: role.id, principalKind: 'user', principalId: user.id, limitations: {} });
  return { id: user.id as string, login, password, name: `E2E ${label} ${t}` };
}

/** A bearer token with the editor role: a principal that is not an administrator, so review rules apply to it. */
export async function editorToken(label: string) {
  const res = await manage('POST', '/tokens', { name: `${label}-${tag()}`, kind: 'token', roles: ['editor'] });
  return res.secret as string;
}

/** A management API call as another principal; never throws, answers the status and the parsed body. */
export async function asToken(secret: string, method: string, path: string, body?: unknown) {
  const res = await fetch(`${base}/api/manage/v1${path}`, {
    method,
    headers: { Authorization: `Bearer ${secret}`, ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  let json: any;
  try { json = text ? JSON.parse(text) : undefined; } catch { json = undefined; }
  return { status: res.status, json, text, code: json?.error?.code as string | undefined };
}

/** The review chip text in the editor header ("In review", "Approved", "Changes requested"). */
export const chip = (page: Page, text: string) => page.locator('.h-badge', { hasText: new RegExp(`^${text}$`) }).first();

/** The editor's History or Review side panel. */
export async function openTab(page: Page, name: string) {
  await page.getByRole('tab', { name, exact: true }).click();
}

export const publishButton = (page: Page) => page.getByRole('button', { name: 'Publish', exact: true });

export async function expectReview(id: string, state: string) {
  await expect.poll(async () => (await manage('GET', `/items/${id}/review`)).state).toBe(state);
}
