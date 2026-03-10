import React from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'
import {
  LayoutDashboard, Inbox, List, ExternalLink, Plus, LayoutTemplate, BookOpen,
} from 'lucide-react'
import { cn } from '@/lib/utils'

const NAV_ITEMS = [
  { href: '/',          label: 'Dashboard', icon: LayoutDashboard },
  { href: '/inbox',     label: 'Inbox',     icon: Inbox },
  { href: '/tasks',     label: 'Tasks',     icon: List },
  { href: '/templates', label: 'Templates', icon: LayoutTemplate },
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
    queryFn: () => api.getInbox(),
    refetchInterval: 5000,
  })
  const inboxCount = inboxData?.items?.length ?? 0

  const { data: knowledgeData } = useQuery({
    queryKey: ['knowledge-pending-count'],
    queryFn: () => api.listKnowledge({ status: 'pending' }),
    refetchInterval: 30000,
  })
  const knowledgePendingCount = knowledgeData?.items?.length ?? 0

  const isActive = (href: string) => {
    if (href === '/') return pathname === '/'
    // Don't match /create for /tasks prefix
    if (href === '/tasks' && pathname === '/create') return false
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

        {/* New Task button */}
        <div className="px-3 pt-3">
          <Link
            to="/create"
            className={cn(
              'flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
              pathname === '/create'
                ? 'bg-primary text-primary-foreground'
                : 'bg-primary/10 text-primary hover:bg-primary/20',
            )}
          >
            <Plus className="h-4 w-4" />
            New Task
          </Link>
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
              badge={label === 'Inbox' ? inboxCount : label === 'Knowledge' ? knowledgePendingCount : undefined}
            />
          ))}
        </nav>

        {/* Footer */}
        <div className="border-t px-3 py-3">
          <a
            href={`${window.location.protocol}//${window.location.hostname}:8233`}
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-3 rounded-lg px-3 py-2 text-xs text-muted-foreground hover:bg-sidebar-accent/50 hover:text-foreground transition-colors"
          >
            <ExternalLink className="h-3.5 w-3.5" />
            Temporal UI
          </a>
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
