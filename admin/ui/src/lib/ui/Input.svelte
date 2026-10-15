<script lang="ts">
  import type { HTMLInputAttributes } from 'svelte/elements';

  type Props = Omit<HTMLInputAttributes, 'value'> & {
    value?: string | number | null;
    invalid?: boolean;
    el?: HTMLInputElement | null;
  };

  let { value = $bindable(''), invalid = false, el = $bindable(null), type = 'text', class: cls = '', ...rest }: Props = $props();
</script>

<input
  bind:this={el}
  class="h-input {cls}"
  {type}
  bind:value
  aria-invalid={invalid || undefined}
  {...rest}
/>
