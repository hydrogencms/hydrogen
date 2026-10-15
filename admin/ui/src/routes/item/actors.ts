import { api } from '../../lib/api/client';
import { session } from '../../lib/auth/session.svelte';
import { t } from '../../lib/i18n';

// The directory is listed once per session, and only for people who may read it.
let users: Promise<Map<string, string>> | undefined;
session.onClear(() => {
  users = undefined;
});
const loadUsers = () =>
  (users ??= session.can('user.manage')
    ? api
        .get('/users', { query: { limit: 200 } })
        .then((r) => new Map(r.items.map((u) => [u.id, u.name || u.login])))
        .catch(() => new Map<string, string>())
    : Promise.resolve(new Map<string, string>()));

/** A person-readable name for a recorded actor such as `user:<id>`, `agent:<id>` or `system`. */
export async function actorName(actor: string | undefined): Promise<string> {
  const [kind, id = ''] = (actor ?? '').split(':');
  if (kind === 'user') {
    if (id && id === session.principal?.id) return session.principal.name || t('editor.who.user');
    return (await loadUsers()).get(id) ?? t('editor.who.user');
  }
  if (kind === 'agent') return t('editor.who.agent');
  return t('editor.who.system');
}
