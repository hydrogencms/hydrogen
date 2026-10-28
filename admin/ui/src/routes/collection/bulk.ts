import { ApiError } from '../../lib/api/client';
import { t } from '../../lib/i18n';

export interface BulkResult {
  id: string;
  name: string;
  ok: boolean;
  message: string;
}

export type Outcome = { ok: true } | { ok: false; error: unknown };

export function errText(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'referenced') {
      const referrers = e.body?.referrers;
      return t('collections.bulk.referenced', { n: String(Array.isArray(referrers) ? referrers.length : 0) });
    }
    if (e.status === 403) return t('app.forbidden');
    return e.message;
  }
  return e instanceof Error ? e.message : String(e);
}

export function bulkResults(targets: readonly { id: string; name?: string }[], outcomes: readonly Outcome[]): BulkResult[] {
  return targets.map((r, i) => {
    const outcome = outcomes[i]!;
    return { id: r.id, name: r.name || r.id, ok: outcome.ok, message: outcome.ok ? '' : errText(outcome.error) };
  });
}
