'use client'
import type { LiveTemplateProps } from '@hydrogencms/next'
import type { Page } from '@/lib/types'
import { Body } from '@/components/body'

// A client component on purpose: in draft mode the adapter re-renders it with the admin's working document.
export default function PageTemplate({ item, locale, preview, metadata }: LiveTemplateProps<Page>) {
  return <Body item={item} locale={locale} preview={preview} alternates={metadata.alternates} />
}
