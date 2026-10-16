import { api } from '../../lib/api/client';
import { normReview, normRow, type QueueRow, type ReviewData, type Reviewer } from './logic';

// The review routes are not in the generated paths yet: the untyped surface of the same wrapper.
type Loose = Record<'get' | 'post', (path: string, o?: object) => Promise<any>>;
const loose = api as unknown as Loose;

const params = (id: string) => ({ params: { id } });

export const reviewApi = {
  async get(id: string, signal?: AbortSignal): Promise<ReviewData> {
    return normReview(await loose.get('/items/{id}/review', { ...params(id), signal }));
  },
  submit(id: string, body: { comment?: string; reviewers?: string[] }): Promise<unknown> {
    return loose.post('/items/{id}/review/submit', { ...params(id), body });
  },
  approve(id: string, draftHash?: string, comment?: string): Promise<unknown> {
    return loose.post('/items/{id}/review/approve', { ...params(id), body: { ...(draftHash ? { draftHash } : {}), ...(comment ? { comment } : {}) } });
  },
  requestChanges(id: string, comment: string): Promise<unknown> {
    return loose.post('/items/{id}/review/request-changes', { ...params(id), body: { comment } });
  },
  withdraw(id: string, comment?: string): Promise<unknown> {
    return loose.post('/items/{id}/review/withdraw', { ...params(id), body: comment ? { comment } : {} });
  },
  comment(id: string, comment: string): Promise<unknown> {
    return loose.post('/items/{id}/review/comments', { ...params(id), body: { comment } });
  },
  async queue(query: { state?: string; type?: string; reviewer?: string; limit?: number; offset?: number }, signal?: AbortSignal): Promise<{ items: QueueRow[]; total?: number; hasMore?: boolean }> {
    const r = await loose.get('/reviews', { query, signal });
    return { items: ((r.items ?? []) as Record<string, unknown>[]).map(normRow), total: r.total, hasMore: r.hasMore };
  },
};

/** Who may decide on an item's review (the reviewer picker's candidates); empty when the list is not allowed or missing. */
export async function approvers(item: string, signal?: AbortSignal): Promise<Reviewer[]> {
  try {
    const r = await loose.get('/reviews/approvers', { query: { item }, signal });
    return ((r.items ?? []) as Record<string, unknown>[])
      .map((x) => ({ ref: typeof x.ref === 'string' ? x.ref : '', name: typeof x.name === 'string' ? x.name : '' }))
      .filter((x) => x.ref);
  } catch (e) {
    if (signal?.aborted) throw e;
    return [];
  }
}
