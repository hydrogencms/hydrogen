import { t } from './i18n';

/** Whether reloading may go ahead: always when nothing is unsaved, otherwise when the user agrees to discard. */
export function confirmDiscard(dirty: boolean, ask: (message: string) => boolean): boolean {
  return !dirty || ask(t('collections.reloadDiscard'));
}
