import type { TreeNode } from '@hydrogencms/next'
import { hydrogen } from '@/lib/hydrogen'

/** The channel name is the locale; the site settings singleton is read in it. */
async function settings(channel: string) {
  try {
    return (await (await hydrogen.clientFor(channel)).singleton('siteSettings', { locale: channel })).data.fields
  } catch {
    return undefined
  }
}

const href = (n: TreeNode) => (n.url ? new URL(n.url).pathname : undefined)

function Item({ n }: { n: TreeNode }) {
  const kids = n.nodes?.filter((k) => k.url) ?? []
  return (
    <li>
      <a href={href(n)}>{n.name}</a>
      {kids.length > 0 && (
        <ul>
          {kids.map((k) => <li key={k.id}><a href={href(k)}>{k.name}</a></li>)}
        </ul>
      )}
    </li>
  )
}

/** Brand, navigation from the page tree of the request's channel (the home page is the brand link), and the language switch. */
export async function Header() {
  const channel = await hydrogen.channel()
  const [tree, s] = await Promise.all([hydrogen.nav({ depth: 2 }), settings(channel)])
  return (
    <header className="site-header">
      <div className="container bar">
        <a className="brand" href={`/${channel}`}>{s?.siteName ?? 'Starter'}</a>
        <nav aria-label="Main">
          <ul>{tree.filter((n) => n.url).map((n) => <Item key={n.id} n={n} />)}</ul>
        </nav>
      </div>
    </header>
  )
}

export async function Footer() {
  const s = await settings(await hydrogen.channel())
  return (
    <footer className="site-footer">
      <div className="container">
        <strong>{s?.siteName}</strong>
        {s?.footerText && <p>{s.footerText}</p>}
      </div>
    </footer>
  )
}
