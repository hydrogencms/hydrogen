import type { TemplateProps } from '@hydrogencms/next'
import type { Article, HydrogenTypes } from '@/lib/types'
import { RichText } from '@/components/richtext'

// A mounted collection: articles live under the "news" mount of the corporate site.
export default async function ArticleTemplate({ item, locale, client }: TemplateProps<Article, HydrogenTypes>) {
  // Cached and tagged through the adapter's client: publishing any article purges this list (tag c:article).
  const more = await client.list('article', { limit: 5 })
  return (
    <article data-template="article" lang={locale}>
      <h1>{item.fields.title}</h1>
      <RichText doc={item.fields.body} />
      <aside>
        <h2>More news</h2>
        <ul>
          {more.data.items.filter((a) => a.id !== item.id).map((a) => (
            <li key={a.id}>{a.url ? <a href={a.url}>{a.fields.title}</a> : a.fields.title}</li>
          ))}
        </ul>
      </aside>
    </article>
  )
}
