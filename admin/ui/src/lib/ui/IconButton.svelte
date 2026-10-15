<script lang="ts">
  import type { HTMLButtonAttributes } from 'svelte/elements';
  import Icon from './Icon.svelte';

  type Props = Omit<HTMLButtonAttributes, 'children'> & {
    icon: string;
    /** Accessible name (also the tooltip). Required. */
    label: string;
    variant?: 'ghost' | 'secondary' | 'danger';
    size?: 'sm' | 'md' | 'lg';
  };

  let { icon, label, variant = 'ghost', size = 'md', type = 'button', class: cls = '', ...rest }: Props = $props();
</script>

<button class="h-iconbutton h-iconbutton--{variant} h-iconbutton--{size} {cls}" {type} aria-label={label} title={label} {...rest}>
  <Icon name={icon} size={size === 'sm' ? 16 : 18} />
</button>
