<script lang="ts">
  import '../app.css'
  import Header from '$lib/Header.svelte'
  import Footer from '$lib/Footer.svelte'

  let { data, children } = $props()
</script>

<a class="skip" href="#main">Skip to content</a>
<Header channel={data.channel} nav={data.nav} siteName={data.settings?.siteName} />
<main id="main">{@render children()}</main>
<Footer settings={data.settings} />
