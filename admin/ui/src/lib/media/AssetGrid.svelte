<script lang="ts">
  import Badge from '../ui/Badge.svelte';
  import { t } from '../i18n';
  import '../messages/media';
  import Thumb from './Thumb.svelte';
  import { dimensions, formatSize } from './logic';
  import type { AssetRow } from './upload';

  let {
    rows,
    selected,
    onpick,
    label = t('media.assets'),
  }: {
    rows: AssetRow[];
    /** The highlighted asset: the one open in the library, or the one already chosen in the picker. */
    selected?: string;
    onpick: (row: AssetRow) => void;
    label?: string;
  } = $props();

  // One tab stop for the whole grid: arrows move between cards (left/right by one, up/down by a row).
  let list = $state<HTMLUListElement>();
  let active = $state('');
  const stop = $derived(rows.some((r) => r.id === active) ? active : rows.some((r) => r.id === selected) ? selected : rows[0]?.id);

  function key(e: KeyboardEvent) {
    const cards = [...(list?.querySelectorAll<HTMLButtonElement>('.h-assetcard') ?? [])];
    const at = cards.indexOf(document.activeElement as HTMLButtonElement);
    if (at < 0) return;
    const top = cards[0]?.offsetTop;
    const cols = Math.max(1, cards.filter((c) => c.offsetTop === top).length);
    let to = at;
    switch (e.key) {
      case 'ArrowRight': to = at + 1; break;
      case 'ArrowLeft': to = at - 1; break;
      case 'ArrowDown': to = at + cols; break;
      case 'ArrowUp': to = at - cols; break;
      case 'Home': to = 0; break;
      case 'End': to = cards.length - 1; break;
      default: return;
    }
    e.preventDefault();
    const next = cards[Math.min(cards.length - 1, Math.max(0, to))];
    next?.focus();
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<ul class="h-assetgrid" aria-label={label} bind:this={list} onkeydown={key}>
  {#each rows as r (r.id)}
    <li>
      <button
        type="button"
        class="h-assetcard"
        aria-current={selected === r.id ? 'true' : undefined}
        tabindex={stop === r.id ? 0 : -1}
        onfocus={() => (active = r.id)}
        onclick={() => onpick(r)}>
        <Thumb file={r.file} />
        <span class="h-assetcard__name">{r.name}</span>
        <span class="h-assetcard__meta h-muted">
          {[r.file ? dimensions(r.file) : '', r.file ? formatSize(r.file.size) : ''].filter(Boolean).join(' · ')}
          {#if r.status === 'draft'}<Badge>{t('media.draft')}</Badge>{/if}
        </span>
      </button>
    </li>
  {/each}
</ul>
