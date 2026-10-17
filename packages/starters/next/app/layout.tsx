import { Suspense } from 'react'
import type { Metadata } from 'next'
import { Footer, Header } from '@/components/site'
import './globals.css'

export const metadata: Metadata = { title: { default: 'Starter', template: '%s' } }

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <a className="skip" href="#main">Skip to content</a>
        <Suspense fallback={<div className="site-header" />}><Header /></Suspense>
        <main id="main">{children}</main>
        <Suspense fallback={null}><Footer /></Suspense>
      </body>
    </html>
  )
}
