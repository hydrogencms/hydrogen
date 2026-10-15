import { ApiError } from './api/client';
import { t } from './i18n';
import { toast } from './ui/toast.svelte';

/** An error as one line: a 403 reads "You can't do this: <policy>", others show the server's message. */
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    return e.status === 403 && e.code === 'forbidden'
      ? `${t('app.forbidden')}: ${t('app.forbiddenPolicy', { policy: e.message })}`
      : e.message;
  }
  return e instanceof Error ? e.message : String(e);
}

/** Toast a 4xx the screen did not map to a field. 5xx and network errors were already toasted by the api client. */
export function reportError(e: unknown): void {
  if (!(e instanceof ApiError)) throw e;
  if (e.status < 500 && e.status > 0) toast.error(errorMessage(e));
}

export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

/** A random password from an unambiguous alphabet (rejection sampling: no modulo bias). */
export function generatePassword(length = 20): string {
  const alphabet = 'abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789-_';
  const limit = 256 - (256 % alphabet.length);
  let out = '';
  while (out.length < length) {
    for (const b of crypto.getRandomValues(new Uint8Array(length * 2))) {
      if (b < limit && out.length < length) out += alphabet[b % alphabet.length];
    }
  }
  return out;
}
