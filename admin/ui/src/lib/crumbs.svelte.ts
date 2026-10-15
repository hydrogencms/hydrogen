export interface Crumb {
  label: string;
  /** In-app path (no prefix). The last crumb is the current page and has none. */
  href?: string;
}

/** The breadcrumb trail shown in the top bar. Screens declare it with `<Breadcrumbs items>` or `<PageHeader breadcrumbs>`. */
class Crumbs {
  items = $state.raw<Crumb[]>([]);
  #owner = 0;

  /** Sets the trail and returns a release function (only the latest owner can clear). */
  set(items: Crumb[]): () => void {
    const me = ++this.#owner;
    this.items = items;
    return () => {
      if (this.#owner === me) this.items = [];
    };
  }
}

export const crumbs = new Crumbs();
