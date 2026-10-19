<script lang="ts">
  import type { SiteSettings } from '$lib/types'
  let { settings }: { settings?: SiteSettings } = $props()
</script>

<footer class="site-footer">
  <div class="container">
    <strong>{settings?.siteName}</strong>
    {#if settings?.footerText}<p>{settings.footerText}</p>{/if}
  </div>
</footer>
