import { channels } from '$lib/channels'

/** `[locale=locale]`: a route segment that is one of the channels. */
export const match = (param: string): boolean => channels.includes(param)
