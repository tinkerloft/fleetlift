import { SunMoon, Sun, Moon } from 'lucide-react'
import { useTheme, type Theme } from '@/hooks/useTheme'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

const CYCLE: Theme[] = ['system', 'light', 'dark']

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

export function ThemeToggle() {
  const { theme, setTheme } = useTheme()
  const Icon = ICONS[theme]
  const label = LABELS[theme]

  function handleClick() {
    const next = CYCLE[(CYCLE.indexOf(theme) + 1) % CYCLE.length]
    setTheme(next)
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          onClick={handleClick}
          title={`Theme: ${label}`}
          className={cn(
            'flex h-9 w-9 items-center justify-center rounded-lg',
            'text-muted-foreground hover:bg-sidebar-accent/50 hover:text-foreground transition-colors',
          )}
          aria-label={`Theme: ${label}`}
        >
          <Icon className="h-4 w-4" />
        </button>
      </TooltipTrigger>
      <TooltipContent side="right">
        Theme: {label}
      </TooltipContent>
    </Tooltip>
  )
}
