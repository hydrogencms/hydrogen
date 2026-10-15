import { notFound } from 'next/navigation'
import type { Metadata } from 'next'
import { channels } from '@/lib/channels'
import { posts } from '@/lib/posts'

export const instant = false
export const metadata: Metadata = { title: 'Blog' }

export default async function BlogIndex({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params
  if (!channels.includes(locale)) notFound()
  const items = await posts(locale)
  return (
    <section className="container prose" lang={locale}>
      <h1>Blog</h1>
      <ul className="posts">
        {items.map((p) => (
          <li key={p.id}>
            <h2><a href={`/${locale}/blog/${p.fields.slug}`}>{p.fields.title}</a></h2>
            {p.fields.excerpt && <p>{p.fields.excerpt}</p>}
          </li>
        ))}
      </ul>
    </section>
  )
}
