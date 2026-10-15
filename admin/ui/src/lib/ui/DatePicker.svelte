<script lang="ts">
  import type { HTMLInputAttributes } from 'svelte/elements';

  type Props = Omit<HTMLInputAttributes, 'value' | 'type'> & {
    /** ISO text: `2026-10-15`, `2026-10-15T09:30` or `09:30`. */
    value?: string;
    type?: 'date' | 'datetime-local' | 'time';
    invalid?: boolean;
  };
  let { value = $bindable(''), type = 'date', invalid = false, class: cls = '', ...rest }: Props = $props();
</script>

<input class="h-input h-date {cls}" {type} bind:value aria-invalid={invalid || undefined} {...rest} />
