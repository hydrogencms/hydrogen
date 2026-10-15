import { hydrogen } from '@/lib/hydrogen'

// Pages: the CMS decides which page lives at which URL.
export const instant = false
export default hydrogen.page()
export const generateMetadata = hydrogen.metadata()
