<script lang="ts">
  type Link = { id: string; name: string; href: string; kids?: Link[] }
  let { channel, nav, siteName }: { channel: string; nav: Link[]; siteName?: string } = $props()
</script>

<header class="site-header">
  <div class="container bar">
    <a class="brand" href="/{channel}">{siteName ?? 'Starter'}</a>
    <nav aria-label="Main">
      <ul>
        {#each nav as n (n.id)}
          <li>
            <a href={n.href}>{n.name}</a>
            {#if n.kids?.length}
              <ul>{#each n.kids as k (k.id)}<li><a href={k.href}>{k.name}</a></li>{/each}</ul>
            {/if}
          </li>
        {/each}
      </ul>
    </nav>
  </div>
</header>
