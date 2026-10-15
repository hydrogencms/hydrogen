import type { TreeNode } from '@hydrogencms/next'
import { hydrogen } from '@/lib/hydrogen'

function List({ nodes }: { nodes: TreeNode[] }) {
  return (
    <ul>
      {nodes.map((n) => (
        <li key={n.id}>
          {n.url ? <a href={n.url}>{n.name}</a> : n.name}
          {n.nodes?.length ? <List nodes={n.nodes} /> : null}
        </li>
      ))}
    </ul>
  )
}

// The navigation of the request's channel: GET /tree through the adapter, cached and tagged nav:<site>.
export async function Nav() {
  const nodes = await hydrogen.nav({ depth: 2 })
  return <nav aria-label="Main"><List nodes={nodes} /></nav>
}
