<script lang="ts">
  import '../../lib/messages/review';
  import { ApiError } from '../../lib/api/client';
  import { errorMessage } from '../../lib/errors';
  import { t } from '../../lib/i18n';
  import Button from '../../lib/ui/Button.svelte';
  import Chip from '../../lib/ui/Chip.svelte';
  import Combobox, { type ComboOption } from '../../lib/ui/Combobox.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Textarea from '../../lib/ui/Textarea.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import { approvers, reviewApi } from './api';
  import { bumpReview } from './revision.svelte';
  import { addReviewer, commentProblem, submitBody, type ReviewData, type Reviewer } from './logic';

  let {
    id,
    review,
    onchange,
    before,
  }: {
    id: string;
    review: ReviewData | null;
    /** A transition went through (or the state turned out stale). Every review view reloads through `bumpReview` anyway. */
    onchange?: () => void;
    /** Runs before a submit (the editor saves unsaved edits); false aborts. */
    before?: () => Promise<boolean>;
  } = $props();

  const current = $derived(review?.state ?? 'none');
  const showSubmit = $derived(!!review?.canSubmit && (current === 'none' || current === 'changes_requested'));
  const showDecide = $derived(!!review?.canApprove && current === 'in_review');
  const showWithdraw = $derived(!!review?.canWithdraw && current === 'in_review');

  let busy = $state(false);
  let dialog = $state<'submit' | 'request'>('submit');
  let isOpen = $state(false);
  let comment = $state('');
  let reviewers = $state<Reviewer[]>([]);
  let picked = $state<ComboOption | null>(null);
  let shown = $state(false);
  let error = $state('');

  const problem = $derived(commentProblem(comment, dialog === 'request'));
  const fieldError = $derived(shown && problem ? t(problem === 'required' ? 'review.commentRequired' : 'review.commentTooLong') : '');

  const changed = () => {
    bumpReview();
    onchange?.();
  };

  function fail(e: unknown) {
    if (!(e instanceof ApiError) || e.status >= 500 || e.status === 0) throw e;
    if (e.code === 'review_state') {
      toast.error(t('review.invalidState'));
      changed();
    } else if (e.code === 'review_stale') {
      toast.error(t('review.stale'));
      changed();
    } else if (e.code === 'four_eyes') error = t('review.fourEyes');
    else error = errorMessage(e);
  }

  async function run(fn: () => Promise<unknown>, done: string): Promise<boolean> {
    busy = true;
    error = '';
    try {
      await fn();
      toast.success(done);
      changed();
      return true;
    } catch (e) {
      fail(e);
      if (!isOpen && error) toast.error(error);
      return false;
    } finally {
      busy = false;
    }
  }

  async function open(kind: 'submit' | 'request') {
    if (kind === 'submit' && before && !(await before())) return;
    comment = '';
    reviewers = [];
    picked = null;
    shown = false;
    error = '';
    dialog = kind;
    isOpen = true;
    if (kind === 'submit') loadCandidates();
  }

  // Who may decide on this item: the picker's only candidates (nothing is listed for people who cannot see them).
  let candidates = $state.raw<Reviewer[]>([]);
  let loadingCandidates: Promise<void> = Promise.resolve();
  function loadCandidates() {
    loadingCandidates = approvers(id).then((r) => {
      candidates = r;
    });
  }

  async function send() {
    shown = true;
    if (problem) return;
    const ok = await run(
      () => (dialog === 'submit' ? reviewApi.submit(id, submitBody(comment, reviewers)) : reviewApi.requestChanges(id, comment.trim())),
      dialog === 'submit' ? t('review.submitted') : t('review.changesRequested'),
    );
    if (ok) isOpen = false;
  }

  async function search(q: string): Promise<ComboOption[]> {
    await loadingCandidates;
    const needle = q.trim().toLowerCase();
    return candidates
      .filter((r) => !reviewers.some((x) => x.ref === r.ref) && (!needle || r.name.toLowerCase().includes(needle)))
      .slice(0, 20)
      .map((r) => ({ value: r.ref, label: r.name, hint: r.ref.startsWith('group:') ? t('review.group') : undefined }));
  }
  function pick(o: ComboOption | null) {
    if (o) reviewers = addReviewer(reviewers, { ref: o.value, name: o.label });
    picked = null;
  }
</script>

{#if showSubmit}<Button disabled={busy} onclick={() => void open('submit')}>{t('review.submit')}</Button>{/if}
{#if showDecide}
  <Button variant="primary" loading={busy && !isOpen} onclick={() => void run(() => reviewApi.approve(id, review?.draftHash), t('review.approved'))}>{t('review.approve')}</Button>
  <Button disabled={busy} onclick={() => void open('request')}>{t('review.requestChanges')}</Button>
{/if}
{#if showWithdraw}
  <Button variant="ghost" disabled={busy} onclick={() => void run(() => reviewApi.withdraw(id), t('review.withdrawn'))}>{t('review.withdraw')}</Button>
{/if}

<Dialog bind:open={isOpen} title={dialog === 'request' ? t('review.requestTitle') : t('review.submitTitle')} size="sm" dismissible={!busy}>
  <form
    id="review-form"
    class="h-stack"
    onsubmit={(e) => {
      e.preventDefault();
      void send();
    }}
  >
    <p>{dialog === 'request' ? t('review.requestText') : t('review.submitText')}</p>
    <Field label={t('review.comment')} description={dialog === 'request' ? undefined : t('review.commentOptional')} error={fieldError} required={dialog === 'request'}>
      {#snippet children(c)}<Textarea {...c} bind:value={comment} rows={4} />{/snippet}
    </Field>
    {#if dialog === 'submit'}
      <Field label={t('review.reviewers')} description={t('review.reviewersHint')}>
        {#snippet children(c)}
          <Combobox {...c} bind:value={picked} {search} placeholder={t('review.reviewersSearch')} onselect={pick} minChars={0} />
        {/snippet}
      </Field>
      {#if reviewers.length}
        <div class="h-row">
          {#each reviewers as r (r.ref)}
            <Chip label={r.name || r.ref} variant="neutral" icon={r.ref.startsWith('group:') ? 'users' : 'user'} onremove={() => (reviewers = reviewers.filter((x) => x.ref !== r.ref))} removeLabel={t('review.reviewerRemove', { name: r.name || r.ref })} />
          {/each}
        </div>
      {/if}
    {/if}
    {#if error}<p class="h-alert" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <Button variant="ghost" disabled={busy} onclick={() => (isOpen = false)}>{t('common.cancel')}</Button>
    <Button type="submit" form="review-form" variant="primary" loading={busy}>{dialog === 'request' ? t('review.sendChanges') : t('review.send')}</Button>
  {/snippet}
</Dialog>
