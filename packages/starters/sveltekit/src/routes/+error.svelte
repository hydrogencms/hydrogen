<script lang="ts">
  import { page } from '$app/state'
</script>

{#if page.status === 404}
  <section class="container notfound" data-template="not-found">
    <p class="code">404</p>
    <h1>Page not found</h1>
    <p>The page you are looking for does not exist or has moved.</p>
    <a class="button" href="/">Back to the home page</a>
  </section>
{:else}
  <section class="container notfound">
    <p class="code">{page.status}</p>
    <h1>{page.error?.message}</h1>
  </section>
{/if}
