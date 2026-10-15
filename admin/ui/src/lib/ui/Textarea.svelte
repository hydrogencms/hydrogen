<script lang="ts">
  import type { HTMLTextareaAttributes } from 'svelte/elements';

  type Props = Omit<HTMLTextareaAttributes, 'value'> & { value?: string | null; invalid?: boolean };

  let { value = $bindable(''), invalid = false, rows = 4, class: cls = '', ...rest }: Props = $props();
</script>

<textarea class="h-input h-textarea {cls}" {rows} bind:value aria-invalid={invalid || undefined} {...rest}></textarea>
