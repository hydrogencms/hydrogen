import { ApiError } from '../api/client';
import { errorMessage } from '../errors';
import { t } from '../i18n';
import './../messages/media';
import { formatSize, maxUpload, pickType, whyRejected, type FileValue, type UploadSpec } from './logic';
import { mediaApi, uploadFile } from './upload';

export interface UploadEntry {
  id: number;
  name: string;
  state: 'queued' | 'uploading' | 'done' | 'exists' | 'error' | 'cancelled';
  progress: number;
  message: string;
  /** The item created (or found) for the file. */
  item?: string;
}

export interface UploadTarget {
  /** The upload types the files may go to. */
  types: { name: string; upload?: UploadSpec }[];
  preferred?: string;
  folder?: string;
  /** An asset already holding these bytes: the file is not added twice. Awaited per file. */
  known: (sha: string, type: string) => Promise<{ id: string; name: string } | undefined> | { id: string; name: string } | undefined;
  /** An entry finished with this item (new or existing). Not called for an upload that was cancelled or whose owner is gone. */
  ondone: (item: string, file: FileValue, type: string, existing: boolean) => void;
}

const CONCURRENCY = 3;
const finished = (e: UploadEntry) => e.state !== 'queued' && e.state !== 'uploading';
const isAbort = (e: unknown) => e instanceof DOMException && e.name === 'AbortError';

/** A queue of uploads (three at a time), each: upload the bytes, then create the asset item. */
export class Uploads {
  entries = $state<UploadEntry[]>([]);
  #n = 0;
  #active = 0;
  #waiting: Array<() => void> = [];
  #master = new AbortController();
  #each = new Map<number, AbortController>();
  /** Items made by this batch, by `type:sha`: the same bytes twice make one item. */
  #made = new Map<string, { id: string; name: string }>();
  #chains = new Map<string, Promise<unknown>>();

  get busy() {
    return this.entries.some((e) => !finished(e));
  }

  /** "2 of 5 uploaded, 1 failed": empty while nothing was queued. For one persistent live region. */
  get summary(): string {
    const list = this.entries.filter((e) => e.state !== 'cancelled');
    if (!list.length) return '';
    const done = list.filter((e) => e.state === 'done' || e.state === 'exists').length;
    const failed = list.filter((e) => e.state === 'error').length;
    return failed ? t('media.summaryFailed', { done, total: list.length, failed }) : t('media.summary', { done, total: list.length });
  }

  /** Drops the finished entries. Nothing finished: nothing is written (safe to call from an effect). */
  clearFinished() {
    if (!this.entries.some(finished)) return;
    this.entries = this.entries.filter((e) => !finished(e));
  }

  /** Stops one queued or running upload. */
  cancel(id: number) {
    this.#each.get(id)?.abort();
  }

  /** Stops everything in flight (a picker closing, a screen leaving); the queue can be used again. */
  abort() {
    const old = this.#master;
    this.#master = new AbortController();
    old.abort();
  }

  destroy() {
    this.abort();
  }

  add(files: Iterable<File>, target: UploadTarget): Promise<void[]> {
    return Promise.all([...files].map((f) => this.#one(f, target)));
  }

  /** Changes one entry in place: progress ticks touch a single object, not the whole list. */
  #patch(id: number, p: Partial<UploadEntry>) {
    const e = this.entries.find((x) => x.id === id);
    if (e) Object.assign(e, p);
  }

  async #slot(signal: AbortSignal) {
    while (this.#active >= CONCURRENCY) {
      await new Promise<void>((resolve, reject) => {
        const wake = () => {
          signal.removeEventListener('abort', onAbort);
          resolve();
        };
        const onAbort = () => {
          this.#waiting = this.#waiting.filter((w) => w !== wake);
          reject(new DOMException('aborted', 'AbortError'));
        };
        this.#waiting.push(wake);
        signal.addEventListener('abort', onAbort, { once: true });
      });
    }
    if (signal.aborted) throw new DOMException('aborted', 'AbortError');
    this.#active++;
  }

  #release() {
    this.#active--;
    this.#waiting.shift()?.();
    if (this.#active === 0 && !this.#waiting.length) {
      this.#made.clear();
      this.#chains.clear();
    }
  }

  /** Runs `fn` after every earlier call with the same key finished (so two identical files cannot both create an item). */
  #serial<T>(key: string, fn: () => Promise<T>): Promise<T> {
    const run = (this.#chains.get(key) ?? Promise.resolve()).then(fn, fn);
    this.#chains.set(key, run.catch(() => {}));
    return run;
  }

  async #one(file: File, target: UploadTarget): Promise<void> {
    const id = ++this.#n;
    this.entries.push({ id, name: file.name, state: 'queued', progress: 0, message: t('media.queued') });
    const ctl = new AbortController();
    this.#each.set(id, ctl);
    const master = this.#master.signal;
    const follow = () => ctl.abort();
    master.addEventListener('abort', follow, { once: true });
    const live = () => !ctl.signal.aborted;
    const limit = maxUpload();
    try {
      const type = pickType(target.types, file, target.preferred, limit);
      if (!type) {
        const why = whyRejected(target.types, file, limit);
        const max = limit !== undefined && file.size > limit ? limit : undefined;
        this.#patch(id, {
          state: 'error',
          message: why === 'size' ? (max !== undefined ? t('media.tooBigLimit', { name: file.name, max: formatSize(max) }) : t('media.tooBig', { name: file.name })) : t('media.notAccepted', { name: file.name }),
        });
        return;
      }
      await this.#slot(ctl.signal);
      try {
        this.#patch(id, { state: 'uploading', message: t('media.uploading') });
        const up = await uploadFile(file, { type, signal: ctl.signal, onProgress: (p) => this.#patch(id, { progress: p }) });
        // One item per file content within the batch: later copies wait for the first one's item.
        const r = await this.#serial(`${type}:${up.file.id}`, async () => {
          if (!live()) throw new DOMException('aborted', 'AbortError');
          const key = `${type}:${up.file.id}`;
          let known = this.#made.get(key);
          // Only bytes the server already held can belong to an item already.
          if (!known && up.existing) known = await target.known(up.file.id, type);
          if (known) return { item: known.id, name: known.name, existing: true };
          const made = await mediaApi.create(type, up.file, { folder: target.folder });
          this.#made.set(key, { id: made.id, name: up.file.name });
          return { item: made.id, name: up.file.name, existing: false };
        });
        if (!live()) return this.#patch(id, { state: 'cancelled', message: t('media.cancelled') });
        if (r.existing) {
          this.#patch(id, { state: 'exists', progress: 1, message: t('media.exists', { name: r.name }), item: r.item });
        } else {
          this.#patch(id, { state: 'done', progress: 1, message: up.existing ? t('media.doneExisting') : t('media.done'), item: r.item });
        }
        target.ondone(r.item, up.file, type, r.existing || up.existing);
      } finally {
        this.#release();
      }
    } catch (e) {
      if (isAbort(e) || !live()) this.#patch(id, { state: 'cancelled', message: t('media.cancelled') });
      else if (e instanceof ApiError && e.status === 0) this.#patch(id, { state: 'error', message: t('media.networkFailed', { name: file.name }) });
      else this.#patch(id, { state: 'error', message: e instanceof ApiError ? errorMessage(e) : t('media.failed') });
    } finally {
      master.removeEventListener('abort', follow);
      this.#each.delete(id);
    }
  }
}
