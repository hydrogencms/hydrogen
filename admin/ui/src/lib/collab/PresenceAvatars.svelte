<script lang="ts">
  import '../messages/collab';
  import './Presence.css';
  import { t } from '../i18n';
  import { readable } from './marks';
  import type { Participant } from './transport';

  let { others }: { others: Participant[] } = $props();

  const initials = (name: string) => {
    const parts = name.trim().split(/[\s._@-]+/).filter(Boolean);
    return ((([...(parts[0] ?? '?')][0] ?? '') + (parts.length > 1 ? ([...parts[parts.length - 1]!][0] ?? '') : '')) || '?').toUpperCase();
  };
  const shown = $derived(others.slice(0, 5));
  const more = $derived(others.length - shown.length);
  const uid = $props.id();
</script>

{#if others.length}
  <span class="h-collab-avatars" role="group" aria-labelledby={uid + '-l'}>
    <span id={uid + '-l'}>{t('collab.alsoHere')}</span>
    <span class="h-collab-avatars__list">
      {#each shown as p (p.client)}
        <span
          class="h-collab-avatar"
          class:h-collab-avatar--idle={p.idle}
          class:h-collab-avatar--view={!p.edit}
          style:--h-collab={p.color}
          style:--h-collab-bg={readable(p.color).bg}
          style:--h-collab-fg={readable(p.color).fg}
          role="img"
          aria-label={p.name + (p.idle ? ' (' + t('collab.idle') + ')' : '') + (p.edit ? '' : ' (' + t('collab.viewing') + ')')}
          title={p.name + (p.idle ? ' (' + t('collab.idle') + ')' : '') + (p.edit ? '' : ' (' + t('collab.viewing') + ')')}>{initials(p.name)}</span>
      {/each}
      {#if more > 0}<span class="h-collab-avatar" role="img" aria-label={t('collab.more', { n: more })} title={t('collab.more', { n: more })}>+{more}</span>{/if}
    </span>
  </span>
{/if}
