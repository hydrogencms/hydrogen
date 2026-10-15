<svelte:options customElement={{ tag: 'x-hello', shadow: 'none' }} />

<script lang="ts">
  import { emit } from '@hydrogencms/admin-sdk'

  // A field widget: the admin sets value and readonly, and listens for 'change' with { value }.
  let { value = '', readonly = false }: { value?: string; readonly?: boolean } = $props()
  const el = $host()
</script>

<input {value} {readonly} oninput={(e) => emit(el, 'change', { value: e.currentTarget.value })} />
