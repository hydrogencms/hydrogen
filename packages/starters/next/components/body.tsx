import { Blocks, Field } from '@hydrogencms/react'
import type { ReactNode } from 'react'
import { Links, registry } from './blocks'
import type { Block } from '@/lib/types'
import { channels } from '@/lib/channels'

/** The shared layout of the templates: title, optional intro and the body blocks. */
/** The same page in the other languages, from the route's alternates, in the channels' configuration order. */
function Languages({ alternates, locale }: { alternates: { hreflang: string; href: string }[]; locale: string }) {
  const order = (l: string) => (channels.includes(l) ? channels.indexOf(l) : channels.length)
  const list = [...alternates].sort((a, b) => order(a.hreflang) - order(b.hreflang))
  if (list.length < 2) return null
  return (
    <nav className="container langs" aria-label="Language">
      {list.map((a) => (
        <a key={a.hreflang} href={new URL(a.href).pathname} hrefLang={a.hreflang} aria-current={a.hreflang === locale ? 'true' : undefined}>
          {a.hreflang.toUpperCase()}
        </a>
      ))}
    </nav>
  )
}

export function Body({ item, locale, preview, hidden, lead, alternates, children }: {
  item: { links?: Record<string, string>; fields: { title: string; body?: Block[] } }
  locale: string
  preview: boolean
  /** the page opens with a hero that carries the visible heading */
  hidden?: boolean
  lead?: ReactNode
  alternates?: { hreflang: string; href: string }[]
  children?: ReactNode
}) {
  return (
    <Links.Provider value={item.links ?? {}}>
      {alternates && <Languages alternates={alternates} locale={locale} />}
      <article lang={locale}>
        <header className={hidden ? 'sr-only' : 'page-head'}>
          <div className="container">
            <h1><Field path="fields/title" preview={preview}>{item.fields.title}</Field></h1>
            {lead}
          </div>
        </header>
        <Blocks blocks={item.fields.body} registry={registry} path="fields/body" preview={preview} />
        {children}
      </article>
    </Links.Provider>
  )
}
