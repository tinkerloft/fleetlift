import React from 'react'
import { Link, useLocation } from 'react-router-dom'

export function Layout({ children }: { children: React.ReactNode }) {
  const { pathname } = useLocation()
  const nav = [
    { href: '/', label: 'Inbox' },
    { href: '/tasks', label: 'All Tasks' },
  ]
  return (
    <div className="min-h-screen bg-background">
      <header className="border-b px-6 py-3 flex items-center gap-6">
        <span className="font-semibold text-lg">Fleetlift</span>
        <nav className="flex gap-4 text-sm">
          {nav.map(({ href, label }) => (
            <Link
              key={href}
              to={href}
              className={
                pathname === href
                  ? 'text-foreground font-medium'
                  : 'text-muted-foreground hover:text-foreground'
              }
            >
              {label}
            </Link>
          ))}
        </nav>
      </header>
      <main className="px-6 py-6">{children}</main>
    </div>
  )
}
