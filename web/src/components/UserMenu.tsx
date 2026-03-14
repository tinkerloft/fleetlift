import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api } from '@/api/client'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem,
  DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger,
} from './ui/dropdown-menu'
import { LogOut, Users } from 'lucide-react'

function initials(name: string): string {
  return name.split(/\s+/).map(w => w[0]).join('').toUpperCase().slice(0, 2) || '?'
}

export function UserMenu() {
  const navigate = useNavigate()
  const { data: user } = useQuery({
    queryKey: ['me'],
    queryFn: () => api.getMe(),
    staleTime: 60_000,
  })

  const handleSignOut = () => {
    localStorage.removeItem('token')
    navigate('/login')
  }

  const displayName = user?.name || 'User'
  const teamName = user?.teams?.[0]?.name

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left transition-colors hover:bg-sidebar-accent">
          <div className="flex h-7 w-7 items-center justify-center rounded-full bg-blue-600 text-[11px] font-semibold text-white">
            {initials(displayName)}
          </div>
          <div className="flex-1 min-w-0">
            <div className="text-[13px] font-medium truncate">{displayName}</div>
            {teamName && <div className="text-[11px] text-muted-foreground truncate">{teamName}</div>}
          </div>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent side="top" align="start" className="w-56">
        <DropdownMenuLabel className="font-normal">
          <div className="text-sm font-medium">{displayName}</div>
          {user?.email && <div className="text-xs text-muted-foreground">{user.email}</div>}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {user?.teams && user.teams.length > 0 && (
          <>
            <DropdownMenuLabel className="text-xs text-muted-foreground">Teams</DropdownMenuLabel>
            {user.teams.map(t => (
              <DropdownMenuItem key={t.id} disabled>
                <Users className="mr-2 h-4 w-4" />
                <span>{t.name}</span>
                <span className="ml-auto text-xs text-muted-foreground">{t.role}</span>
              </DropdownMenuItem>
            ))}
            <DropdownMenuSeparator />
          </>
        )}
        <DropdownMenuItem onClick={handleSignOut}>
          <LogOut className="mr-2 h-4 w-4" />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
