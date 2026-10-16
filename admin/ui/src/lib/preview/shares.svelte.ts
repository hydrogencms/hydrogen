import { shareApi, shareState, type Share } from './share';

/** The item's share links, kept by the editor (it relays drafts while one is active) and shown in the dialog. */
export class ShareList {
  items = $state.raw<Share[]>([]);
  #item = '';
  #seq = 0;

  get active(): number {
    const now = Date.now();
    return this.items.filter((s) => shareState(s, now) === 'active').length;
  }

  async load(item: string): Promise<void> {
    this.#item = item;
    const seq = ++this.#seq;
    const list = await shareApi.list(item);
    if (seq === this.#seq && item === this.#item) this.items = list;
  }

  reset() {
    this.#seq++;
    this.#item = '';
    this.items = [];
  }
}
