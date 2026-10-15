<script lang="ts">
  import { link, href as toHref } from '../router.svelte';
  import Icon from './Icon.svelte';
  import Avatar from './Avatar.svelte';

  let {
    name,
    sub,
    icon,
    avatar,
    href,
  }: {
    name: string;
    /** Muted second line (slug, key, login). */
    sub?: string;
    /** Type icon on a 32 px tile. */
    icon?: string;
    /** Use an avatar (initials of this name) instead of the icon tile. */
    avatar?: string;
    /** In-app path: the name becomes a real link (keyboard and middle-click). */
    href?: string;
  } = $props();
</script>

<span class="h-name">
  {#if avatar}<Avatar name={avatar} size={32} />{:else if icon}<span class="h-tile"><Icon name={icon} size={16} /></span>{/if}
  <span class="h-name__text">
    {#if href}<a class="h-name__title" use:link href={toHref(href)}>{name}</a>{:else}<span class="h-name__title">{name}</span>{/if}
    {#if sub}<span class="h-name__sub">{sub}</span>{/if}
  </span>
</span>
