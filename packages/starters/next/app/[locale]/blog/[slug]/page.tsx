import { notFound } from 'next/navigation'
import type { Metadata } from 'next'
import { channels } from '@/lib/channels'
import { hydrogen } from '@/lib/hydrogen'
import { postBySlug } from '@/lib/posts'
import PostTemplate from '@/templates/post'

type Props = { params: Promise<{ locale: string; slug: string }> }

export const instant = false

async function load({ params }: Props) {
  const { locale, slug } = await params
  if (!channels.includes(locale)) notFound()
  const item = await postBySlug(await hydrogen.clientFor(locale), locale, slug)
  if (!item) notFound()
  return { item, locale }
}

export async function generateMetadata(props: Props): Promise<Metadata> {
  const { item } = await load(props)
  return { title: item.fields.title, description: item.fields.excerpt }
}

export default async function PostPage(props: Props) {
  const { item, locale } = await load(props)
  const draft = !!(await hydrogen.previewToken())
  return hydrogen.render(PostTemplate, { item, locale, draft, metadata: { title: item.name, alternates: [], locale } })
}
