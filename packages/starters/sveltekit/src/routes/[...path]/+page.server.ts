import { hydrogen } from '$lib/server/hydrogen'
import type { PageServerLoad } from './$types'

// Pages: the CMS decides which page lives at which URL (a redirect or 404 is decided before anything renders).
export const load: PageServerLoad = (event) => hydrogen.load(event)
