/** The channels this deployment serves; the request's host and path prefix pick one (see lib/hydrogen.ts). */
export const channels = (process.env.HYDROGEN_CHANNELS ?? 'corporate_en,corporate_de,corporate_dech,corporate_sg_en,careers_en')
  .split(',')
  .map((s) => s.trim())
  .filter(Boolean)
