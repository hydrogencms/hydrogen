// Pure helpers for the review workflow: state tones, queue tabs, event text, payloads. Server shapes are read
// leniently (`normReview`, `normRow`) so a renamed optional field does not blank the screen.

export type ReviewState = 'none' | 'in_review' | 'approved' | 'changes_requested';
export const STATES: ReviewState[] = ['none', 'in_review', 'approved', 'changes_requested'];
export const MAX_COMMENT = 4000;

export interface ReviewEvent {
  id: string;
  kind: string;
  actor: string;
  actorName: string;
  comment: string;
  at: string;
}

export interface Reviewer {
  /** `user:<id>` or `group:<id>`. */
  ref: string;
  name: string;
}

export interface ReviewData {
  state: ReviewState;
  submittedBy: string;
  submittedByName: string;
  submittedAt: string;
  reviewers: Reviewer[];
  decidedBy: string;
  decidedByName: string;
  decidedAt: string;
  required: boolean;
  /** Hash of the draft the review is about; sent back on approve so a changed draft is refused. */
  draftHash: string;
  canSubmit: boolean;
  canApprove: boolean;
  canWithdraw: boolean;
  events: ReviewEvent[];
}

export interface QueueRow {
  id: string;
  type: string;
  name: string;
  state: ReviewState;
  submittedBy: string;
  submittedByName: string;
  submittedAt: string;
  reviewers: Reviewer[];
}

type Rec = Record<string, unknown>;
const str = (v: unknown) => (typeof v === 'string' ? v : '');
const bool = (v: unknown) => v === true;

export function asState(v: unknown): ReviewState {
  return STATES.includes(v as ReviewState) ? (v as ReviewState) : 'none';
}

export function normReviewers(v: unknown, names: Rec = {}): Reviewer[] {
  if (!Array.isArray(v)) return [];
  return v
    .map((r) => {
      if (typeof r === 'string') return { ref: r, name: str(names[r]) };
      const o = (r ?? {}) as Rec;
      const ref = str(o.ref) || str(o.id) || (o.kind && o.id ? `${str(o.kind)}:${str(o.id)}` : '');
      return { ref, name: str(o.name) };
    })
    .filter((r) => r.ref);
}

export function normEvent(e: Rec, i = 0): ReviewEvent {
  return {
    id: str(e.id) || String(i),
    kind: str(e.kind) || str(e.type) || str(e.event) || str(e.action),
    actor: str(e.actor) || str(e.by),
    actorName: str(e.actorName) || str(e.byName),
    comment: str(e.comment),
    at: str(e.at) || str(e.createdAt),
  };
}

export function normReview(r: Rec): ReviewData {
  const names = (r.reviewerNames ?? {}) as Rec;
  return {
    state: asState(r.state),
    submittedBy: str(r.submittedBy),
    submittedByName: str(r.submittedByName),
    submittedAt: str(r.submittedAt),
    reviewers: normReviewers(r.reviewers, names),
    decidedBy: str(r.decidedBy),
    decidedByName: str(r.decidedByName),
    decidedAt: str(r.decidedAt),
    required: bool(r.required),
    draftHash: str(r.draftHash),
    canSubmit: bool(r.canSubmit),
    canApprove: bool(r.canApprove),
    canWithdraw: bool(r.canWithdraw),
    events: Array.isArray(r.events) ? (r.events as Rec[]).map(normEvent) : [],
  };
}

export function normRow(r: Rec): QueueRow {
  return {
    id: str(r.item) || str(r.itemId) || str(r.id),
    type: str(r.type),
    name: str(r.name),
    state: asState(r.state),
    submittedBy: str(r.submittedBy),
    submittedByName: str(r.submittedByName),
    submittedAt: str(r.submittedAt),
    reviewers: normReviewers(r.reviewers, (r.reviewerNames ?? {}) as Rec),
  };
}

export function stateTone(s: ReviewState): 'neutral' | 'success' | 'warning' | 'danger' | 'info' {
  return s === 'approved' ? 'success' : s === 'in_review' ? 'info' : s === 'changes_requested' ? 'warning' : 'neutral';
}

/** Why Publish is off for an item whose type requires a review; '' when it may go ahead. */
export function publishBlock(required: boolean, state: ReviewState, bypass: boolean): '' | 'needsApproved' {
  return required && state !== 'approved' && !bypass ? 'needsApproved' : '';
}

/** The bypass menu item shows when a review is required, none is approved and the principal may skip it. */
export function canBypass(required: boolean, state: ReviewState, bypass: boolean): boolean {
  return required && state !== 'approved' && bypass;
}

export const TABS = ['assigned', 'in_review', 'changes_requested', 'approved'] as const;
export type QueueTab = (typeof TABS)[number];

export function parseTab(v: string | null): QueueTab {
  return TABS.includes(v as QueueTab) ? (v as QueueTab) : 'assigned';
}

export function tabQuery(tab: QueueTab): { state: string; reviewer?: 'me' } {
  return tab === 'assigned' ? { state: 'in_review', reviewer: 'me' } : { state: tab };
}

export const reviewerRef = (kind: 'user' | 'group', id: string) => `${kind}:${id}`;

/** Adds a reviewer unless already present (order kept). */
export function addReviewer(list: Reviewer[], r: Reviewer): Reviewer[] {
  return list.some((x) => x.ref === r.ref) ? list : [...list, r];
}

export function commentProblem(comment: string, required: boolean): '' | 'required' | 'tooLong' {
  const c = comment.trim();
  if (required && !c) return 'required';
  return [...c].length > MAX_COMMENT ? 'tooLong' : '';
}

/** Body of POST …/review/submit: empty parts are left out. */
export function submitBody(comment: string, reviewers: Reviewer[]): { comment?: string; reviewers?: string[] } {
  const body: { comment?: string; reviewers?: string[] } = {};
  if (comment.trim()) body.comment = comment.trim();
  if (reviewers.length) body.reviewers = reviewers.map((r) => r.ref);
  return body;
}

const KNOWN = ['submitted', 'resubmitted', 'approved', 'changes_requested', 'withdrawn', 'reopened', 'edited', 'comment', 'published', 'discarded'];
export const eventKey = (kind: string) => (KNOWN.includes(kind) ? `review.event.${kind}` : 'review.event.other');

/** Newest first would hide the story: the thread reads oldest to newest. */
export function sortEvents(list: ReviewEvent[]): ReviewEvent[] {
  return list.map((e, i) => ({ e, i })).sort((a, b) => a.e.at.localeCompare(b.e.at) || a.i - b.i).map((x) => x.e);
}

export function displayName(name: string, fallback: string): string {
  return name.trim() || fallback;
}
