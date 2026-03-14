import type React from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'
import {
  Inbox, LayoutTemplate, FileText, Activity, BookOpen,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { UserMenu } from './UserMenu'

const NAV_ITEMS = [
  { href: '/runs',      label: 'Runs',      icon: Activity },
  { href: '/workflows', label: 'Workflows', icon: LayoutTemplate },
  { href: '/inbox',     label: 'Inbox',     icon: Inbox },
  { href: '/reports',   label: 'Reports',   icon: FileText },
  { href: '/knowledge', label: 'Knowledge', icon: BookOpen },
]

function NavLink({ href, label, icon: Icon, active, badge }: {
  href: string
  label: string
  icon: React.FC<{ className?: string }>
  active: boolean
  badge?: number
}) {
  return (
    <Link
      to={href}
      className={cn(
        'flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors',
        active
          ? 'bg-sidebar-accent text-foreground font-medium'
          : 'text-muted-foreground hover:bg-sidebar-accent/50 hover:text-foreground',
      )}
    >
      <Icon className="h-4 w-4 shrink-0" />
      <span className="flex-1">{label}</span>
      {badge != null && badge > 0 && (
        <span className="ml-auto flex h-5 min-w-5 items-center justify-center rounded-full bg-blue-600 px-1.5 text-[10px] font-medium text-white">
          {badge}
        </span>
      )}
    </Link>
  )
}

export function Layout({ children }: { children: React.ReactNode }) {
  const { pathname } = useLocation()

  const { data: inboxData } = useQuery({
    queryKey: ['inbox'],
    queryFn: () => api.listInbox(),
    refetchInterval: 5000,
  })
  const inboxCount = inboxData?.items?.length ?? 0

  const isActive = (href: string) => {
    if (href === '/') return pathname === '/'
    return pathname.startsWith(href)
  }

  return (
    <div className="flex min-h-screen bg-background">
      {/* Sidebar */}
      <aside className="sticky top-0 flex h-screen w-56 shrink-0 flex-col border-r bg-sidebar">
        {/* Logo */}
        <div className="flex h-14 items-center gap-2 border-b px-4">
          <div className="flex h-7 w-7 items-center justify-center rounded-md bg-foreground">
            <span className="text-xs font-bold text-background">FL</span>
          </div>
          <span className="text-sm font-semibold tracking-tight">Fleetlift</span>
        </div>

        {/* Nav */}
        <nav className="flex-1 space-y-1 px-3 py-3">
          {NAV_ITEMS.map(({ href, label, icon }) => (
            <NavLink
              key={href}
              href={href}
              label={label}
              icon={icon}
              active={isActive(href)}
              badge={label === 'Inbox' ? inboxCount : undefined}
            />
          ))}
        </nav>

        {/* Footer */}
        <div className="border-t px-3 py-3">
          <UserMenu />
        </div>
      </aside>

      {/* Main content */}
      <div className="flex flex-1 flex-col min-w-0">
        <main className="flex-1 px-8 py-6">
          {children}
        </main>
      </div>
    </div>
  )
}
