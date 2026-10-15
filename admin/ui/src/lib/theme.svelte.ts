export type Theme = 'system' | 'light' | 'dark';

class ThemeStore {
  value = $state<Theme>('system');

  init() {
    const v = localStorage.getItem('h.theme');
    this.set(v === 'light' || v === 'dark' ? v : 'system');
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

export const theme = new ThemeStore();
