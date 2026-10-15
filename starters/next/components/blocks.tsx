import { createContext, useContext } from 'react'
import { createRegistry, Field, RichText } from '@hydrogencms/react'
import type { BlockComponentProps } from '@hydrogencms/react'
import type { Block } from '@/lib/types'

/** The `links` of the delivered item: item id -> URL, for ref props and rich text links. Provided by the templates. */
export const Links = createContext<Record<string, string>>({})

const cx = (...c: (string | false | undefined)[]) => c.filter(Boolean).join(' ')
type P<N extends Block['type']> = BlockComponentProps<Extract<Block, { type: N }> extends { props?: infer X } ? NonNullable<X> : never>

/** Every component spreads `attrs` on its root: `data-h-block` always, `data-h-path` in preview. */
function Hero({ props: p, attrs }: P<'hero'>) {
  const links = useContext(Links)
  const href = p.ctaLink ? links[p.ctaLink] : p.ctaUrl
  return (
    <section {...attrs} className={cx('hero', `tone-${p.tone ?? 'light'}`)}>
      <div className="container">
        <h2 className="hero-title">{p.heading}</h2>
        {p.subheading && <p className="hero-sub">{p.subheading}</p>}
        {p.ctaLabel && href && <a className="button" href={href}>{p.ctaLabel}</a>}
      </div>
    </section>
  )
}

function RichTextBlock({ props: p, attrs }: P<'richText'>) {
  return (
    <div {...attrs} className="container prose">
      <RichText doc={p.body} links={useContext(Links)} />
    </div>
  )
}

function Cards({ props: p, slots, attrs }: P<'cards'>) {
  return (
    <section {...attrs} className={cx('container cards', `space-${p.space ?? 'md'}`)}>
      {p.heading && <h2>{p.heading}</h2>}
      <div className={`grid cols-${p.columns ?? '3'}`}>{slots.items}</div>
    </section>
  )
}

function Card({ props: p, attrs }: P<'card'>) {
  const links = useContext(Links)
  const href = p.link ? links[p.link] : undefined
  return (
    <article {...attrs} className="card">
      <h3>{href ? <a href={href}>{p.title}</a> : p.title}</h3>
      {p.text && <p>{p.text}</p>}
    </article>
  )
}

function Callout({ props: p, attrs }: P<'callout'>) {
  return (
    <div {...attrs} className="container prose">
      <aside className={cx('callout', `tone-${p.tone ?? 'brand'}`)}><p>{p.text}</p></aside>
    </div>
  )
}

function Image({ props: p, attrs }: P<'image'>) {
  return (
    <div {...attrs} className="container prose">
      <figure className="figure">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img src={p.src} alt={p.alt ?? ''} loading="lazy" />
        {p.caption && <figcaption>{p.caption}</figcaption>}
      </figure>
    </div>
  )
}

/** `shared` is built in: it renders the blocks delivery inlined. */
export const registry = createRegistry({ hero: Hero, richText: RichTextBlock, cards: Cards, card: Card, callout: Callout, image: Image })

export { Field }
