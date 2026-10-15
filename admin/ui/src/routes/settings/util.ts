import { api, ApiError } from '../../lib/api/client';
import { t } from '../../lib/i18n';
import { errorMessage } from '../../lib/errors';
import type { Form } from '../../lib/form.svelte';
import type { LimitDef, PolicyDef } from './limits';

export async function loadCatalog(): Promise<{ policies: PolicyDef[]; limitations: LimitDef[] }> {
  return api.get('/policies');
}

/** Flat message for a form's field errors (shown verbatim under the form). */
export function errorText(e: unknown): string {
  return e instanceof ApiError ? e.message : String(e);
}

export const PAGE = 20;

/** form.submit, with a 403 shown as "You can't do this" plus the policy rather than the bare policy name. */
export async function submitForm<V extends object>(form: Form<V>, fn: (v: V) => Promise<unknown>): Promise<boolean> {
  let denied: ApiError | undefined;
  const ok = await form.submit(async (v) => {
    try {
      return await fn(v);
    } catch (e) {
      if (e instanceof ApiError && e.status === 403 && e.code === 'forbidden') denied = e;
      throw e;
    }
  });
  if (denied) form.formError = errorMessage(denied);
  return ok;
}

/** One page (1-based) of an already loaded list. */
export const pageOf = <T>(rows: T[], page: number): T[] => rows.slice((page - 1) * PAGE, page * PAGE);

/** Case-insensitive "contains" over several fields, for filtering a loaded list. */
export const matchesText = (q: string, ...fields: (string | undefined)[]): boolean => {
  const needle = q.trim().toLowerCase();
  return !needle || fields.some((f) => f?.toLowerCase().includes(needle));
};
