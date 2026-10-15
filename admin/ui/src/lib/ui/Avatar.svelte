<script lang="ts">
  import Icon from './Icon.svelte';
  /** `icon` replaces the initials (tokens, agents, the system). */
  let { name, size = 32, icon }: { name: string; size?: 24 | 28 | 32 | 40; icon?: string } = $props();

  const initials = $derived.by(() => {
    const parts = name.trim().split(/[\s._@-]+/).filter(Boolean);
    if (parts.length === 0) return '?';
    const a = [...parts[0]!][0] ?? '';
    const b = parts.length > 1 ? ([...parts[parts.length - 1]!][0] ?? '') : '';
    return (a + b).toUpperCase();
  });
  // A stable neutral tint per name: 6 low-saturation fills.
  const tint = $derived.by(() => {
    let h = 0;
    for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
    return (h % 6) + 1;
  });
</script>

<span class="h-avatar h-avatar--{size} h-avatar--t{tint}" aria-hidden="true">{#if icon}<Icon name={icon} size={size > 28 ? 16 : 14} />{:else}{initials}{/if}</span>
