<script lang="ts">
  import { Page } from '@hydrogencms/sveltekit/components'
  import { templates } from '$lib/templates'

  let { data } = $props()
</script>

<Page {data} {templates} />
