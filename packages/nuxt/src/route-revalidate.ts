import { defineEventHandler, setResponseStatus, toWebRequest } from 'h3'
import { config } from './context.js'
import { purge } from './delivery.js'
import { MIN_SECRET_LENGTH, SIGNATURE_HEADER, readCapped, verifySignature, webhookTags } from '@hydrogencms/client/server'

const MAX_BODY = 1 << 20

/** `POST /api/hydrogen/revalidate`: the signed webhook receiver; purges the cached responses the event's tags name. */
export default defineEventHandler(async (event) => {
  const fail = (status: number, error: string) => (setResponseStatus(event, status), { error })
  const secret = config().revalidateSecret
  if (event.method !== 'POST') return fail(405, 'method not allowed')
  if (!secret || secret.length < MIN_SECRET_LENGTH) return fail(500, 'revalidateSecret too short')
  const req = toWebRequest(event)
  const body = await readCapped(req, MAX_BODY)
  if (!body) return fail(413, 'body too large')
  if (!(await verifySignature(secret, req.headers.get(SIGNATURE_HEADER), body))) return fail(401, 'invalid signature')
  let parsed: unknown
  try {
    parsed = JSON.parse(new TextDecoder().decode(body))
  } catch {
    return fail(400, 'invalid body')
  }
  const tags = webhookTags(parsed)
  purge(tags)
  return { revalidated: tags }
})
