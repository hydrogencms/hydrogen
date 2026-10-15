import { Suspense } from 'react'
import { Nav } from '@/components/nav'

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body style={{ fontFamily: 'system-ui, sans-serif', margin: '0 auto', maxWidth: 720, padding: 16 }}>
        <header><Suspense fallback={null}><Nav /></Suspense></header>
        <main>{children}</main>
      </body>
    </html>
  )
}
