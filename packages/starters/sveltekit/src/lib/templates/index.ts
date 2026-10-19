import Landing from './Landing.svelte'
import PageTemplate from './Page.svelte'
import Post from './Post.svelte'

/** By content type: what `<Page>` renders for the item's type. */
export const templates = { page: PageTemplate, landing: Landing, post: Post }
