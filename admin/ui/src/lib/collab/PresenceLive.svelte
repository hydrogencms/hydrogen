<script lang="ts">
  import '../messages/collab';
  import { t } from '../i18n';
  import type { Participant } from './transport';

  /** A visually hidden live region: who joined or left, and which field someone started editing. */
  let { others, root }: { others: Participant[]; root: HTMLElement | undefined } = $props();

  let text = $state('');
  let prev = new Map<string, Participant>();
  let first = true;

  const labelOf = (path: string) => {
    const row = root?.querySelector<HTMLElement>(`[data-field="${CSS.escape(path)}"]`);
    const l = row?.querySelector('label, legend, .h-field__label')?.textContent?.trim();
    return l || row?.querySelector('[aria-label]')?.getAttribute('aria-label') || path;
  };

  $effect(() => {
    const next = new Map(others.map((p) => [p.client, p]));
    const said: string[] = [];
    if (!first) {
      for (const [c, p] of next) {
        const was = prev.get(c);
        if (!was) said.push(t('collab.joined', { name: p.name }));
        if (p.focus && p.focus !== was?.focus) said.push(t('collab.editingField', { name: p.name, field: labelOf(p.focus) }));
      }
      for (const [c, p] of prev) if (!next.has(c)) said.push(t('collab.left', { name: p.name }));
    }
    first = false;
    prev = next;
    if (said.length) text = said.join('. ');
  });
</script>

<div class="h-sr" role="status" aria-live="polite" aria-atomic="true" data-testid="collab-live">{text}</div>
