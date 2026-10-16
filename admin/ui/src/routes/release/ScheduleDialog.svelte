<script lang="ts">
  import { untrack } from 'svelte';
  import '../../lib/messages/releases';
  import { ApiError } from '../../lib/api/client';
  import { errorMessage } from '../../lib/errors';
  import { t } from '../../lib/i18n';
  import Button from '../../lib/ui/Button.svelte';
  import DatePicker from '../../lib/ui/DatePicker.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import { defaultSchedule, isoToLocal, localToISO, localZone, scheduleProblem, zoneOffsetLabel } from './logic';

  let {
    open = $bindable(false),
    title,
    text,
    confirm,
    initial,
    onconfirm,
  }: {
    open?: boolean;
    title: string;
    text?: string;
    /** Label of the confirm button. */
    confirm?: string;
    /** An ISO instant to start from (rescheduling). */
    initial?: string;
    /** Receives the chosen instant (ISO, UTC); the dialog closes when it resolves. */
    onconfirm: (at: string) => Promise<void>;
  } = $props();

  const zone = localZone();
  let value = $state('');
  let busy = $state(false);
  let error = $state('');
  let touched = $state(false);

  $effect(() => {
    if (!open) return;
    // Only opening resets the field; a refreshed `initial` must not wipe what is being typed.
    const start = untrack(() => initial);
    value = start ? isoToLocal(start, zone) : defaultSchedule(Date.now(), zone);
    error = '';
    touched = false;
  });

  const iso = $derived(localToISO(value, zone));
  const problem = $derived(scheduleProblem(iso));
  const shown = $derived(touched && problem ? t(`schedule.${problem}`) : '');

  async function go() {
    touched = true;
    if (problem || !iso) return;
    busy = true;
    error = '';
    try {
      await onconfirm(iso);
      open = false;
    } catch (e) {
      if (!(e instanceof ApiError) || e.status >= 500 || e.status === 0) throw e;
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog bind:open {title} size="sm" dismissible={!busy}>
  <form
    id="schedule-form"
    class="h-stack"
    onsubmit={(e) => {
      e.preventDefault();
      void go();
    }}
  >
    {#if text}<p>{text}</p>{/if}
    <Field label={t('schedule.when')} description={t('schedule.zone', { zone: `${zone} (${zoneOffsetLabel(zone, iso ? Date.parse(iso) : Date.now())})` })} error={shown}>
      {#snippet children(c)}<DatePicker {...c} type="datetime-local" bind:value oninput={() => (touched = true)} />{/snippet}
    </Field>
    {#if error}<p class="h-alert" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <Button variant="ghost" disabled={busy} onclick={() => (open = false)}>{t('common.cancel')}</Button>
    <Button type="submit" form="schedule-form" variant="primary" loading={busy}>{confirm ?? t('schedule.confirm')}</Button>
  {/snippet}
</Dialog>
