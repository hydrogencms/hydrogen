import type { NextConfig } from 'next'

const config: NextConfig = {
  // "use cache" + cacheTag: the adapter tags cached CMS responses with Hydrogen's purge tags.
  cacheComponents: true,
  partialPrefetching: true,
}
export default config
