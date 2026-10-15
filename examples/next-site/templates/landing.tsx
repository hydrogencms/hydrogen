import type { TemplateProps } from '@hydrogencms/next'
import type { HydrogenTypes, Landing } from '@/lib/types'

export default function LandingTemplate({ item, locale }: TemplateProps<Landing, HydrogenTypes>) {
  return (
    <section data-template="landing" lang={locale}>
      <h1>{item.fields.title}</h1>
    </section>
  )
}
