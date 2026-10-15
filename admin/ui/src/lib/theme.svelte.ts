export type Theme = 'system' | 'light' | 'dark';

class ThemeStore {
  value = $state<Theme>('system');

  init() {
    const v = localStorage.getItem('h.theme');
    this.set(v === 'light' || v === 'dark' ? v : 'system');
  }

  /** system -> light -> dark -> system. */
  cycle() {
    this.set(themeOrder[(themeOrder.indexOf(this.value) + 1) % themeOrder.length]!);
  }

  set(v: Theme) {
    this.value = v;
    if (v === 'system') {
      localStorage.removeItem('h.theme');
      document.documentElement.removeAttribute('data-theme');
    } else {
      localStorage.setItem('h.theme', v);
      document.documentElement.dataset.theme = v;
    }
  }
}

export const themeOrder: Theme[] = ['system', 'light', 'dark'];

export const theme = new ThemeStore();
