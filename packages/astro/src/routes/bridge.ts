import source from 'virtual:hydrogen/bridge'

export const prerender = false
export const GET = () => new Response(source, { headers: { 'content-type': 'text/javascript; charset=utf-8', 'cache-control': 'public, max-age=300' } })
