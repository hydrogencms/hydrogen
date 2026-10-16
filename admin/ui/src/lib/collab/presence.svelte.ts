import type { CollabConnection, CollabState, Participant } from './transport';

export const IDLE_AFTER = 60_000;

/** Fires `change(true)` after `after` ms without `touch()`, and `change(false)` on the first touch after that. */
export class IdleWatcher {
  idle = false;
  #timer: ReturnType<typeof setTimeout> | undefined;
  constructor(
    private change: (idle: boolean) => void,
    private after = IDLE_AFTER,
  ) {
    this.touch();
  }
  touch() {
    clearTimeout(this.#timer);
    if (this.idle) {
      this.idle = false;
      this.change(false);
    }
    this.#timer = setTimeout(() => this.set(true), this.after);
  }
  /** Idle right away (the tab was hidden). */
  set(idle: boolean) {
    if (idle === this.idle) return;
    this.idle = idle;
    this.change(idle);
  }
  stop() {
    clearTimeout(this.#timer);
  }
}

/** The path a form field reports as its focus: the innermost `[data-field]` around the element. */
export function focusPath(el: EventTarget | null): string | undefined {
  return (el as Element | null)?.closest?.('[data-field]')?.getAttribute('data-field') ?? undefined;
}

/** What the editor shows of the session: who else is here, and whether the session works at all. */
export class Presence {
  others = $state.raw<Participant[]>([]);
  state = $state<CollabState>('idle');
  /** True once the session failed (not while it is merely connecting): the banner shows. */
  unavailable = $derived(this.state === 'unavailable');

  #tr: CollabConnection | null = null;
  #offs: (() => void)[] = [];
  #focus: string | undefined;
  #idle: IdleWatcher | null = null;
  #locale = () => '';

  attach(tr: CollabConnection, locale: () => string) {
    this.detach();
    this.#tr = tr;
    this.#locale = locale;
    this.state = tr.state;
    this.#offs.push(
      tr.onState((s) => (this.state = s)),
      tr.onPresence((c) => (this.others = c.filter((p) => p.client !== tr.client))),
    );
    const w = new IdleWatcher((idle) => this.#send(idle));
    this.#idle = w;
    const touch = () => w.touch();
    const vis = () => (document.hidden ? w.set(true) : w.touch());
    for (const ev of ['keydown', 'pointerdown', 'pointermove', 'wheel']) {
      addEventListener(ev, touch, { passive: true });
      this.#offs.push(() => removeEventListener(ev, touch));
    }
    document.addEventListener('visibilitychange', vis);
    this.#offs.push(() => document.removeEventListener('visibilitychange', vis), () => w.stop());
    this.#send(false);
  }

  /** The form's focus moved to the field at `path` (undefined: out of the form). */
  focus(path: string | undefined) {
    if (path === this.#focus) return;
    this.#focus = path;
    this.#send(this.#idle?.idle ?? false);
  }

  #send(idle: boolean) {
    this.#tr?.presence({ locale: this.#locale(), focus: this.#focus, idle: idle || undefined });
  }

  /** Locale switches re-announce. */
  announce() {
    this.#send(this.#idle?.idle ?? false);
  }

  detach() {
    for (const f of this.#offs.splice(0)) f();
    this.#idle = null;
    this.#tr = null;
    this.others = [];
    this.state = 'idle';
    this.#focus = undefined;
  }
}
