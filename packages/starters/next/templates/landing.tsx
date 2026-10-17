'use client'
import type { LiveTemplateProps } from '@hydrogencms/next'
import type { Landing } from '@/lib/types'
import { Body } from '@/components/body'

export default function LandingTemplate({ item, locale, preview, metadata }: LiveTemplateProps<Landing>) {
  return <Body item={item} locale={locale} preview={preview} alternates={metadata.alternates} hidden={item.fields.body?.[0]?.type === 'hero'} />
}
