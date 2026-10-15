import type { TemplateProps } from '@hydrogencms/next'

// The fallback template: any other page type renders its name and fields.
export default function Generic({ item, locale }: TemplateProps) {
  return (
    <article data-template="generic" lang={locale}>
      <h1>{item.name}</h1>
      <pre>{JSON.stringify(item.fields, null, 2)}</pre>
    </article>
  )
}
