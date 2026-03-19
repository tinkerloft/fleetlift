import type React from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'
import {
  Inbox, LayoutTemplate, FileText, Activity, BookOpen, Heart, Settings,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { UserMenu } from './UserMenu'
import { ThemeToggle } from './ThemeToggle'

const NAV_ITEMS = [
  { href: '/runs',      label: 'Runs',      icon: Activity },
  { href: '/workflows', label: 'Workflows', icon: LayoutTemplate },
  { href: '/inbox',     label: 'Inbox',     icon: Inbox },
  { href: '/reports',   label: 'Reports',   icon: FileText },
  { href: '/knowledge', label: 'Knowledge', icon: BookOpen },
  { href: '/system',    label: 'System',    icon: Heart },
  { href: '/settings',  label: 'Settings',  icon: Settings },
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
        'flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-[12px] transition-colors',
        active
          ? 'bg-accent/10 text-accent font-medium'
          : 'text-muted-foreground hover:bg-sidebar-accent hover:text-foreground',
      )}
    >
      <Icon className="h-3.5 w-3.5 shrink-0" />
      <span className="flex-1">{label}</span>
      {badge != null && badge > 0 && (
        <span className="ml-auto flex h-4 min-w-4 items-center justify-center rounded-full bg-accent px-1 text-[9px] font-medium text-white font-mono">
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
      <aside className="sticky top-0 flex h-screen w-52 shrink-0 flex-col border-r bg-sidebar">
        {/* Logo */}
        <div className="flex h-12 items-center gap-2.5 border-b px-4">
          <div className="flex h-6 w-6 items-center justify-center rounded bg-accent">
            <span className="text-[10px] font-bold text-white font-mono tracking-tight">FL</span>
          </div>
          <span className="text-[13px] font-semibold tracking-tight text-foreground">Fleetlift</span>
        </div>

        {/* Nav */}
        <nav className="flex-1 space-y-0.5 px-2 py-2.5">
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
        <div className="border-t px-2 py-2.5 flex flex-col gap-0.5">
          <ThemeToggle />
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
