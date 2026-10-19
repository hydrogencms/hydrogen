<script lang="ts">
  let { data } = $props()
</script>

<svelte:head><title>Blog</title></svelte:head>

<section class="container prose" lang={data.locale}>
  <h1>Blog</h1>
  <ul class="posts">
    {#each data.posts as p (p.id)}
      <li>
        <h2><a href="/{data.locale}/blog/{p.slug}">{p.title}</a></h2>
        {#if p.excerpt}<p>{p.excerpt}</p>{/if}
      </li>
    {/each}
  </ul>
</section>
