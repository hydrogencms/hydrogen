export type ToastKind = 'info' | 'success' | 'error';
export interface ToastItem {
  id: number;
  kind: ToastKind;
  text: string;
  timeout: number;
  /** A button in the toast ("Undo"); running it dismisses the toast. */
  action?: { label: string; run: () => void };
}

class Toasts {
  items = $state<ToastItem[]>([]);
  #n = 0;

  push(kind: ToastKind, text: string, timeout = kind === 'error' ? 8000 : 4000, action?: ToastItem['action']) {
    const id = ++this.#n;
    this.items = [...this.items, { id, kind, text, timeout, action }];
    if (timeout > 0) setTimeout(() => this.dismiss(id), timeout);
    return id;
  }
  info = (text: string) => this.push('info', text);
  success = (text: string) => this.push('success', text);
  error = (text: string) => this.push('error', text);
  dismiss(id: number) {
    this.items = this.items.filter((t) => t.id !== id);
  }
}

export const toast = new Toasts();
