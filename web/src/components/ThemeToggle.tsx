import { SunMoon, Sun, Moon, Check, ChevronUp } from 'lucide-react'
import { useTheme, type Theme } from '@/hooks/useTheme'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

const LABELS: Record<Theme, string> = {
  system: 'System',
  light: 'Light',
  dark: 'Dark',
}

const ICONS: Record<Theme, React.FC<{ className?: string }>> = {
  system: SunMoon,
  light: Sun,
  dark: Moon,
}

const ITEMS: Theme[] = ['system', 'light', 'dark']

export function ThemeToggle() {
  const { theme, setTheme } = useTheme()
  const Icon = ICONS[theme]

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-muted-foreground transition-colors hover:bg-sidebar-accent hover:text-foreground">
          <Icon className="h-4 w-4 shrink-0" />
          <span className="flex-1 text-[13px] font-medium">{LABELS[theme]}</span>
          <ChevronUp className="h-3 w-3 opacity-50" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent side="top" align="start" className="w-44">
        {ITEMS.map((t) => {
          const ItemIcon = ICONS[t]
          return (
            <DropdownMenuItem key={t} onClick={() => setTheme(t)}>
              <ItemIcon className="mr-2 h-4 w-4" />
              <span>{LABELS[t]}</span>
              {theme === t && <Check className="ml-auto h-4 w-4" />}
            </DropdownMenuItem>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
