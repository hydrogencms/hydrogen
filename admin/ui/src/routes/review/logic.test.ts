import { describe, expect, it } from 'vitest';
import { addReviewer, canBypass, commentProblem, eventKey, normReview, normRow, parseTab, publishBlock, sortEvents, stateTone, submitBody, tabQuery } from './logic';

describe('publish gating', () => {
  it('blocks unless approved or bypassing', () => {
    expect(publishBlock(false, 'none', false)).toBe('');
    expect(publishBlock(true, 'none', false)).toBe('needsApproved');
    expect(publishBlock(true, 'in_review', false)).toBe('needsApproved');
    expect(publishBlock(true, 'approved', false)).toBe('');
    expect(publishBlock(true, 'none', true)).toBe('');
  });
  it('offers the bypass only when it matters', () => {
    expect(canBypass(true, 'in_review', true)).toBe(true);
    expect(canBypass(true, 'approved', true)).toBe(false);
    expect(canBypass(false, 'none', true)).toBe(false);
    expect(canBypass(true, 'none', false)).toBe(false);
  });
});

describe('queue', () => {
  it('maps tabs to queries', () => {
    expect(tabQuery('assigned')).toEqual({ state: 'in_review', reviewer: 'me' });
    expect(tabQuery('approved')).toEqual({ state: 'approved' });
    expect(parseTab('changes_requested')).toBe('changes_requested');
    expect(parseTab('x')).toBe('assigned');
  });
  it('reads rows leniently', () => {
    const r = normRow({ item: 'i1', type: 'article', name: 'A', state: 'in_review', submittedByName: 'Ann', reviewers: ['user:1'] });
    expect(r).toMatchObject({ id: 'i1', state: 'in_review', submittedByName: 'Ann' });
    expect(r.reviewers).toEqual([{ ref: 'user:1', name: '' }]);
    expect(normRow({ state: 'bogus' }).state).toBe('none');
  });
});

describe('review data', () => {
  it('normalises the status and events', () => {
    const r = normReview({ state: 'approved', required: true, canApprove: true, events: [{ kind: 'comment', actorName: 'B', comment: 'ok', at: '2026-01-01T00:00:00Z' }] });
    expect(r.state).toBe('approved');
    expect(r.canApprove).toBe(true);
    expect(r.canSubmit).toBe(false);
    expect(r.events[0]).toMatchObject({ kind: 'comment', actorName: 'B', comment: 'ok' });
  });
  it('reads the thread oldest first', () => {
    const e = (id: string, at: string) => ({ id, kind: 'comment', actor: '', actorName: '', comment: '', at });
    expect(sortEvents([e('b', '2026-02-01'), e('a', '2026-01-01')]).map((x) => x.id)).toEqual(['a', 'b']);
  });
  it('names the event text, with a fallback', () => {
    expect(eventKey('approved')).toBe('review.event.approved');
    expect(eventKey('weird')).toBe('review.event.other');
  });
  it('tones', () => {
    expect(stateTone('approved')).toBe('success');
    expect(stateTone('none')).toBe('neutral');
  });
});

describe('forms', () => {
  it('validates comments', () => {
    expect(commentProblem('  ', true)).toBe('required');
    expect(commentProblem('', false)).toBe('');
    expect(commentProblem('x'.repeat(4001), false)).toBe('tooLong');
  });
  it('builds the submit body and dedupes reviewers', () => {
    const a = { ref: 'user:1', name: 'A' };
    expect(addReviewer(addReviewer([], a), a)).toHaveLength(1);
    expect(submitBody(' ', [])).toEqual({});
    expect(submitBody(' hi ', [a, { ref: 'group:2', name: 'G' }])).toEqual({ comment: 'hi', reviewers: ['user:1', 'group:2'] });
  });
});

describe('server fields', () => {
  it('reads the draft hash, tolerating its absence, and the edited event', () => {
    expect(normReview({ state: 'in_review', draftHash: 'abc' }).draftHash).toBe('abc');
    expect(normReview({ state: 'in_review' }).draftHash).toBe('');
    expect(eventKey('edited')).toBe('review.event.edited');
  });
  it('names queue reviewers from reviewerNames', () => {
    const r = normRow({ item: 'i1', state: 'in_review', reviewers: ['user:1', 'group:2'], reviewerNames: { 'user:1': 'Ann' } });
    expect(r.reviewers).toEqual([{ ref: 'user:1', name: 'Ann' }, { ref: 'group:2', name: '' }]);
  });
});
