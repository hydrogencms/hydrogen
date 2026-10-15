import type { TemplateProps } from '@hydrogencms/next'
import type { HydrogenTypes, Page } from '@/lib/types'

export default function PageTemplate({ item, locale }: TemplateProps<Page, HydrogenTypes>) {
  return (
    <article data-template="page" lang={locale}>
      <h1>{item.fields.title}</h1>
      {item.fields.intro && <p className="intro">{item.fields.intro}</p>}
    </article>
  )
}
