// Management API helper for scripts/next-e2e.sh: node cms.mjs <command> [args]. Prints JSON.
//   seed                 the multisite content (two sites, translations, a news mount)
//   rename <id> <slug>   change the slug of an item and publish it
//   retitle <id> <title> change the English title and publish
//   token <channel> <item> <ttl seconds>   mint a preview token
//   draft                create a page that is never published, place it, mint a preview token
const base = `${process.env.CMS_URL}/api/manage/v1`
const auth = { Authorization: `Bearer ${process.env.MANAGE_TOKEN}`, 'Content-Type': 'application/json' }

async function api(method, path, body, want = 200) {
  const res = await fetch(base + path, { method, headers: auth, body: body === undefined ? undefined : JSON.stringify(body) })
  const text = await res.text()
  if (res.status !== want) throw new Error(`${method} ${path}: ${res.status} ${text}`)
  return text ? JSON.parse(text) : undefined
}

async function item(type, locale, fields, more = {}) {
  const v = await api('POST', `/collections/${type}/items`, { locale, fields, alwaysAvailable: false }, 201)
  await api('POST', `/items/${v.id}/publish`, {})
  for (const [loc, fl] of Object.entries(more)) {
    await api('PATCH', `/items/${v.id}`, { locale: loc, fields: fl })
    await api('POST', `/items/${v.id}/publish`, { locales: [loc] })
  }
  return v.id
}

const place = async (site, id, parent) =>
  (await api('POST', `/pages/sites/${site}/nodes`, { kind: 'page', item: id, ...(parent ? { parent } : {}) }, 201)).id
const t = (title) => ({ title })

const commands = {
  async seed() {
    const home = await item('page', 'en', t('Home'), { de: t('Startseite') })
    const homeNode = await place('corporate', home)
    const about = await item('page', 'en', { title: 'About', intro: 'About Acme' }, { de: t('Uber uns'), 'de-CH': t('Uber eus') })
    const aboutNode = await place('corporate', about, homeNode)
    const contact = await item('page', 'en', t('Contact'), { de: t('Kontakt') })
    await place('corporate', contact, homeNode)
    const team = await item('page', 'en', t('Team'))
    await place('corporate', team, aboutNode)
    const cart = await item('page', 'en', t('Cart'))
    await place('corporate', cart, homeNode)
    await api('POST', '/pages/sites/corporate/nodes', { kind: 'mount', parent: homeNode, segment: 'news', collection: 'article', route: '{slug}' }, 201)
    const article = await item('article', 'en', t('Hello'))
    const jobs = await item('page', 'en', t('Jobs'))
    const jobsNode = await place('careers', jobs)
    const roles = await item('page', 'en', t('Open roles'))
    await place('careers', roles, jobsNode)
    return { home, about, aboutNode, contact, team, article, jobs, roles }
  },
  async rename(id, slug) {
    await api('PATCH', `/items/${id}`, { locale: 'en', fields: { slug } })
    await api('POST', `/items/${id}/publish`, {})
    return { id, slug }
  },
  async retitle(id, title) {
    await api('PATCH', `/items/${id}`, { locale: 'en', fields: { title } })
    await api('POST', `/items/${id}/publish`, {})
    return { id, title }
  },
  async token(channel, item, ttl) {
    return api('POST', '/preview-tokens', { channel, item, ttl: Number(ttl) })
  },
  async draft(aboutNode) {
    const v = await api('POST', '/collections/page/items', { locale: 'en', fields: t('Secret launch') }, 201)
    await api('POST', '/pages/sites/corporate/nodes', { kind: 'page', item: v.id, parent: aboutNode }, 201)
    const { token } = await api('POST', '/preview-tokens', { channel: 'corporate_en', item: v.id })
    return { id: v.id, token }
  },
}

const [cmd, ...args] = process.argv.slice(2)
if (!commands[cmd]) {
  console.error('usage: cms.mjs seed|rename|retitle|token|draft')
  process.exit(2)
}
console.log(JSON.stringify(await commands[cmd](...args)))
