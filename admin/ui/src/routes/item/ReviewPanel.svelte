<script lang="ts">
  import '../../lib/messages/review';
  import { ApiError } from '../../lib/api/client';
  import { errorMessage } from '../../lib/errors';
  import { t } from '../../lib/i18n';
  import Avatar from '../../lib/ui/Avatar.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Textarea from '../../lib/ui/Textarea.svelte';
  import Time from '../../lib/ui/Time.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import { reviewApi } from '../review/api';
  import { commentProblem, displayName, eventKey, sortEvents, stateTone, type ReviewData } from '../review/logic';
  import { bumpReview, reviewRev } from '../review/revision.svelte';
  import type { ItemData } from './model';

  let { item }: { item: ItemData } = $props();

  // Props are getters on the parent's object: key the load on derived scalars, not on `item` itself.
  const id = $derived(item.id);
  const stamp = $derived(`${item.version}|${item.updatedAt}|${item.status}`);
  let tick = $state(0);
  let review = $state.raw<ReviewData | null>(null);
  let failed = $state(false);

  $effect(() => {
    void stamp;
    void tick;
    void reviewRev.n;
    const ctl = new AbortController();
    failed = false;
    reviewApi
      .get(id, ctl.signal)
      .then((r) => {
        review = r;
      })
      .catch((e) => {
        if (ctl.signal.aborted) return;
        failed = true;
        if (e instanceof ApiError && e.status < 500 && e.status > 0) return;
        throw e;
      });
    return () => ctl.abort();
  });

  let text = $state('');
  let posting = $state(false);
  let shown = $state(false);
  const problem = $derived(commentProblem(text, true));

  async function post() {
    shown = true;
    if (problem) return;
    posting = true;
    try {
      await reviewApi.comment(id, text.trim());
      text = '';
      shown = false;
      bumpReview();
    } catch (e) {
      if (!(e instanceof ApiError) || e.status >= 500 || e.status === 0) throw e;
      toast.error(errorMessage(e));
    } finally {
      posting = false;
    }
  }

  const events = $derived(review ? sortEvents(review.events) : []);
  const who = (name: string) => displayName(name, t('review.someone'));
</script>

<div class="h-review" data-review-panel>
  {#if failed}
    <p class="h-alert" role="alert">{t('review.failed')} <Button size="sm" onclick={() => tick++}>{t('app.retry')}</Button></p>
  {:else if !review}
    <Skeleton lines={4} height="1.5rem" />
  {:else}
    <div class="h-review__state">
      <Badge tone={stateTone(review.state)} dot>{t(`review.state.${review.state}`)}</Badge>
      {#if review.required}<span class="h-muted">{t('review.required')}</span>{/if}
    </div>
    {#if review.state !== 'none' && review.submittedAt}
      <p class="h-muted">{t('review.by', { name: who(review.submittedByName) })} · <Time at={review.submittedAt} /></p>
    {/if}
    {#if review.reviewers.length}
      <p class="h-muted">{t('review.assigned', { names: review.reviewers.map((r) => r.name || t('review.someone')).join(', ') })}</p>
    {/if}
    {#if (review.state === 'approved' || review.state === 'changes_requested') && review.decidedAt}
      <p class="h-muted">{t('review.decided', { name: who(review.decidedByName) })} · <Time at={review.decidedAt} /></p>
    {/if}

    <h3 class="h-review__h">{t('review.thread')}</h3>
    {#if events.length === 0}
      <p class="h-muted">{t('review.noEvents')}</p>
    {:else}
      <ul class="h-feed">
        {#each events as e (e.id)}
          <li class="h-feed__item">
            <Avatar name={who(e.actorName)} size={28} />
            <div class="h-feed__text">
              <span>{t(eventKey(e.kind), { name: who(e.actorName), kind: e.kind })}</span>
              {#if e.comment}<span class="h-review__comment">{e.comment}</span>{/if}
              <span class="h-feed__sub"><Time at={e.at} /></span>
            </div>
          </li>
        {/each}
      </ul>
    {/if}

    <form
      class="h-stack"
      onsubmit={(e) => {
        e.preventDefault();
        void post();
      }}
    >
      <label class="h-sr" for="review-comment-{id}">{t('review.addComment')}</label>
      <Textarea id="review-comment-{id}" bind:value={text} rows={3} placeholder={t('review.commentPlaceholder')} invalid={shown && !!problem} />
      <div><Button type="submit" loading={posting} disabled={!text.trim()}>{t('review.post')}</Button></div>
    </form>
  {/if}
</div>

<style>
  .h-review {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-3);
  }
  .h-review__state {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--h-space-2);
  }
  .h-review__h {
    margin: var(--h-space-2) 0 0;
    font-size: var(--h-text-sm, 0.875rem);
  }
  .h-review__comment {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
