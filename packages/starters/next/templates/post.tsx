'use client'
import type { LiveTemplateProps } from '@hydrogencms/next'
import type { Post } from '@/lib/types'
import { Field } from '@hydrogencms/react'
import { Body } from '@/components/body'

export default function PostTemplate({ item, locale, preview }: LiveTemplateProps<Post>) {
  const date = new Date(item.publishedAt)
  return (
    <Body
      item={item}
      locale={locale}
      preview={preview}
      lead={
        <p className="meta">
          <time dateTime={item.publishedAt}>{isNaN(+date) ? '' : date.toLocaleDateString(locale, { dateStyle: 'long', timeZone: 'UTC' })}</time>
          {item.fields.excerpt && <> · <Field path="fields/excerpt" preview={preview}>{item.fields.excerpt}</Field></>}
        </p>
      }
    />
  )
}
