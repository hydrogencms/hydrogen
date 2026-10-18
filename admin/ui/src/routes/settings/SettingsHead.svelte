<script lang="ts">
  import '../../lib/messages/settings';
  import type { Snippet } from 'svelte';
  import { session } from '../../lib/auth/session.svelte';
  import { t } from '../../lib/i18n';
  import { router } from '../../lib/router.svelte';
  import PageHeader from '../../lib/ui/PageHeader.svelte';
  import Tabs from '../../lib/ui/Tabs.svelte';

  type Section = 'users' | 'groups' | 'roles' | 'tokens' | 'audit' | 'system';

  let { section, primary }: { section: Section; primary?: Snippet } = $props();

  const all: { id: Section; label: () => string; policy: string }[] = [
    { id: 'users', label: () => t('nav.users'), policy: 'user.manage' },
    { id: 'groups', label: () => t('nav.groups'), policy: 'user.manage' },
    { id: 'roles', label: () => t('nav.roles'), policy: 'role.manage' },
    { id: 'tokens', label: () => t('nav.tokens'), policy: 'token.manage' },
    { id: 'audit', label: () => t('nav.audit'), policy: 'audit.read' },
    { id: 'system', label: () => t('nav.system'), policy: 'system.read' },
  ];
  const tabs = $derived(all.filter((s) => session.can(s.policy)).map((s) => ({ id: s.id, label: s.label() })));
  const current = $derived(all.find((s) => s.id === section)!.label());
</script>

<PageHeader title={t('nav.settings')} breadcrumbs={[{ label: t('nav.settings') }, { label: current }]} meta={t('settings.meta')} {primary} />
<Tabs variant="underline" label={t('nav.settings')} {tabs} value={section} onchange={(id) => void router.navigate(`/settings/${id}`)} />
