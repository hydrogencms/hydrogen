<script lang="ts">
  import type { HTMLButtonAttributes } from 'svelte/elements';
  import Icon from './Icon.svelte';
  import { t } from '../i18n';

  type Props = Omit<HTMLButtonAttributes, 'children'> & {
    label: string;
    /** `applied`: accent-filled (an active filter); `ghost`: dashed "+ Add filter"; `neutral`: plain. */
    variant?: 'applied' | 'ghost' | 'neutral';
    icon?: string;
    /** Chevron after the label (opens an editor). */
    menu?: boolean;
    /** Shows an × that calls this; the label of the × is `removeLabel`. */
    onremove?: () => void;
    removeLabel?: string;
  };

  let { label, variant = 'neutral', icon, menu = false, onremove, removeLabel, type = 'button', class: cls = '', ...rest }: Props = $props();
</script>

<span class="h-chip h-chip--{variant} {cls}">
  <button class="h-chip__main" {type} {...rest}>
    {#if icon}<Icon name={icon} size={14} />{/if}{label}{#if menu}<Icon name="chevron-down" size={14} />{/if}
  </button>
  {#if onremove}
    <button class="h-chip__x" type="button" aria-label={removeLabel ?? `${t('common.clear')}: ${label}`} onclick={onremove}><Icon name="x" size={14} /></button>
  {/if}
</span>
