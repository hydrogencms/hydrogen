import { hydrogen } from '@/lib/hydrogen'

// Target of the CMS webhook (events "*"): verifies the signature and revalidates the event's cache tags.
export const POST = hydrogen.revalidate()
