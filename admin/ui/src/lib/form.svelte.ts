import { ApiError, type FieldError } from './api/client';
import { router } from './router.svelte';
import { t } from './i18n';

/**
 * Form state for one editable object: values, field errors keyed by path (`title`, `body[2].text`; an error
 * path may be prefixed, see `prefix`), dirty tracking, pending submit and a leave guard.
 */
export class Form<V extends object> {
  values: V = $state() as V;
  errors = $state<Record<string, string>>({});
  /** Error not tied to a field (e.g. a conflict message). */
  formError = $state('');
  pending = $state(false);

  #base = $state.raw('');
  #prefix: string;

  /** `prefix` is stripped from API error paths, e.g. 'fields.' when the API reports `fields.title`. */
  constructor(initial: V, opts: { prefix?: string } = {}) {
    this.#prefix = opts.prefix ?? '';
    this.values = initial;
    this.#base = JSON.stringify(initial);
  }

  dirty = $derived(JSON.stringify($state.snapshot(this.values)) !== this.#base);

  /** Replace the values and treat them as saved (after load or a successful save). */
  reset(values: V) {
    this.values = values;
    this.#base = JSON.stringify(values);
    this.errors = {};
    this.formError = '';
  }

  /** Mark the current values as saved. */
  markSaved() {
    this.#base = JSON.stringify($state.snapshot(this.values));
  }

  error(path: string): string | undefined {
    return this.errors[path];
  }

  /** Errors at or under `path` (for groups and lists). */
  errorsUnder(path: string): Record<string, string> {
    const out: Record<string, string> = {};
    for (const [k, v] of Object.entries(this.errors)) if (k === path || k.startsWith(path + '.') || k.startsWith(path + '[')) out[k] = v;
    return out;
  }

  clearError(path: string) {
    if (path in this.errors) {
      const { [path]: _, ...rest } = this.errors;
      this.errors = rest;
    }
  }

  setErrors(fields: FieldError[] | undefined) {
    const e: Record<string, string> = {};
    for (const f of fields ?? []) {
      const p = this.#prefix && f.path.startsWith(this.#prefix) ? f.path.slice(this.#prefix.length) : f.path;
      e[p] ??= f.message;
    }
    this.errors = e;
  }

  /**
   * Run `fn(values)`; disables resubmission while pending, maps an API error's `fields` to `errors`.
   * Resolves true on success, false on failure (errors are set; other errors are rethrown).
   */
  async submit(fn: (values: V) => Promise<unknown>): Promise<boolean> {
    if (this.pending) return false;
    this.pending = true;
    this.formError = '';
    this.errors = {};
    try {
      await fn($state.snapshot(this.values) as V);
      this.markSaved();
      return true;
    } catch (e) {
      if (e instanceof ApiError && e.status >= 400 && e.status < 500) {
        this.setErrors(e.fields);
        this.formError = e.fields?.length ? t('form.invalid') : e.message;
        return false;
      }
      throw e;
    } finally {
      this.pending = false;
    }
  }

  /**
   * Call once while a component initialises: warns on tab close and on in-app navigation while dirty.
   * Returns the cleanup (also run automatically by `$effect`).
   */
  guard(): void {
    $effect(() => {
      const unblock = router.block(() => !this.dirty || confirm(t('form.unsaved')));
      const untrack = router.trackDirty(() => this.dirty);
      const before = (e: BeforeUnloadEvent) => {
        if (this.dirty) e.preventDefault();
      };
      addEventListener('beforeunload', before);
      return () => {
        unblock();
        untrack();
        removeEventListener('beforeunload', before);
      };
    });
  }
}
