import type { ReactNode } from 'react'
import type { RichText as Doc } from '@/lib/types'

type Node = NonNullable<Doc>

const inline = (n: Node, i: number): ReactNode => {
  let out: ReactNode = n.text
  for (const m of n.marks ?? []) {
    if (m.type === 'bold') out = <strong>{out}</strong>
    else if (m.type === 'italic') out = <em>{out}</em>
    else if (m.type === 'link' && typeof m.attrs?.href === 'string') out = <a href={m.attrs.href}>{out}</a>
  }
  return <span key={i}>{out}</span>
}

const block = (n: Node, i: number): ReactNode => {
  const kids = n.content?.map((c, j) => (c.type === 'text' ? inline(c, j) : block(c, j)))
  switch (n.type) {
    case 'paragraph': return <p key={i}>{kids}</p>
    case 'heading': return n.attrs?.level === 3 ? <h3 key={i}>{kids}</h3> : <h2 key={i}>{kids}</h2>
    case 'bulletList': return <ul key={i}>{kids}</ul>
    case 'orderedList': return <ol key={i}>{kids}</ol>
    case 'listItem': return <li key={i}>{kids}</li>
    default: return <div key={i}>{kids}</div>
  }
}

/** Minimal ProseMirror renderer for the sample. */
export function RichText({ doc }: { doc?: Doc }) {
  return doc ? <>{doc.content?.map(block)}</> : null
}
