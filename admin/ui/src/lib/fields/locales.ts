import { channels } from '../channels.svelte';

export type LocaleState = 'published' | 'draft' | 'fallback' | 'missing';

/** How an item stands in one locale: published, draft-only, served by fallback (always available), or missing. */
export function localeState(
  locale: string,
  item: { locales: string[]; draftParts: string[]; alwaysAvailable?: boolean; mainLocale?: string },
): LocaleState {
  if (item.locales.includes(locale)) return 'published';
  if (item.draftParts.includes(locale)) return 'draft';
  if (item.alwaysAvailable && item.mainLocale !== locale) return 'fallback';
  return 'missing';
}

/** The languages the admin offers: the active channel's, else every channel's (first appearance order). */
export function knownLocales(): string[] {
  const active = channels.languages;
  if (active) return active;
  const seen: string[] = [];
  for (const c of channels.items) for (const l of c.languages) if (!seen.includes(l)) seen.push(l);
  return seen;
}

/** Locales shown for one item: the offered ones plus any the item already has. */
export function itemLocales(offered: string[], item: { locales: string[]; draftParts: string[]; mainLocale?: string; availableLocales?: string[] }): string[] {
  const out = [...offered];
  for (const l of [item.mainLocale ?? '', ...item.locales, ...item.draftParts, ...(item.availableLocales ?? [])]) {
    if (l && l !== 'shared' && !out.includes(l)) out.push(l);
  }
  return out;
}

export type OverlayNote = 'derived' | 'overrides' | 'draft' | '';

/** How an inheriting locale stands: published through its parent only, has published overrides, or draft overrides. */
export function overlayNote(locale: string, item: { locales: string[]; draftParts: string[]; availableLocales?: string[] }): OverlayNote {
  if (item.draftParts.includes(locale)) return 'draft';
  if (item.locales.includes(locale)) return 'overrides';
  if (item.availableLocales?.includes(locale)) return 'derived';
  return '';
}
