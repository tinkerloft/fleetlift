import { useState, useEffect, useCallback } from 'react'

export type Theme = 'system' | 'light' | 'dark'

const STORAGE_KEY = 'fleetlift:theme'

function readStorage(): Theme {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v === 'light' || v === 'dark' || v === 'system') return v
  } catch (_) {}
  return 'system'
}

function writeStorage(theme: Theme) {
  try {
    localStorage.setItem(STORAGE_KEY, theme)
  } catch (_) {}
}

function applyTheme(theme: Theme, mql: MediaQueryList) {
  const dark = theme === 'dark' || (theme === 'system' && mql.matches)
  document.documentElement.classList.toggle('dark', dark)
}

export function useTheme(): { theme: Theme; setTheme: (theme: Theme) => void } {
  const [theme, setThemeState] = useState<Theme>(readStorage)

  const setTheme = useCallback((next: Theme) => {
    setThemeState(next)
    writeStorage(next)
  }, [])

  useEffect(() => {
    const mql = window.matchMedia('(prefers-color-scheme: dark)')
    applyTheme(theme, mql)

    if (theme !== 'system') return

    const handler = (e: { matches: boolean }) => {
      document.documentElement.classList.toggle('dark', e.matches)
    }
    mql.addEventListener('change', handler)
    return () => mql.removeEventListener('change', handler)
  }, [theme])

  return { theme, setTheme }
}
