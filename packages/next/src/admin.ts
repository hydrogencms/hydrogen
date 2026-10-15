/** Why a live preview may not connect to this admin origin, if it may not: the visitor's browser has to reach it. */
export function adminOriginWarning(origin: string): string | undefined {
  const u = new URL(origin)
  const h = u.hostname
  if (h === 'localhost' || h === '127.0.0.1' || h === '[::1]') return undefined
  const internal = !h.includes('.') || /\.(internal|local|lan|localdomain|home\.arpa)$/.test(h) ||
    /^(10\.|127\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.|169\.254\.)/.test(h)
  if (u.protocol !== 'https:' && !internal)
    return `adminOrigin ${origin} is not https: a preview frame on an https site will not connect. Set adminOrigin to the admin's public https origin.`
  if (internal)
    return `adminOrigin ${origin} looks like an internal host: the live preview connects only if the editor's browser reaches the admin at exactly this origin. Set adminOrigin to the admin's public origin.`
  return undefined
}
