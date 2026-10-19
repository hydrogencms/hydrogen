import { getContext, setContext } from 'svelte'

/** The `links` of the delivered item (item id -> URL), for ref props and rich text links; provided by the templates. */
const KEY = Symbol('links')
export const setLinks = (links: () => Record<string, string>) => setContext(KEY, links)
export const getLinks = (): Record<string, string> => getContext<(() => Record<string, string>) | undefined>(KEY)?.() ?? {}
