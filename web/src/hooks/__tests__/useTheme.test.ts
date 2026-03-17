import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useTheme } from '../useTheme'

// matchMedia mock
function mockMatchMedia(matches: boolean) {
  const listeners: ((e: { matches: boolean }) => void)[] = []
  const mql = {
    matches,
    addEventListener: vi.fn((_: string, fn: (e: { matches: boolean }) => void) => listeners.push(fn)),
    removeEventListener: vi.fn((_: string, fn: (e: { matches: boolean }) => void) => {
      const i = listeners.indexOf(fn)
      if (i !== -1) listeners.splice(i, 1)
    }),
    _listeners: listeners,
    _fire: (matches: boolean) => listeners.forEach(fn => fn({ matches })),
  }
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn(() => mql),
  })
  return mql
}

beforeEach(() => {
  localStorage.clear()
  document.documentElement.classList.remove('dark')
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useTheme', () => {
  it('defaults to "system" when localStorage is empty', () => {
    mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    expect(result.current.theme).toBe('system')
  })

  it('restores saved "light" from localStorage', () => {
    localStorage.setItem('fleetlift:theme', 'light')
    mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    expect(result.current.theme).toBe('light')
  })

  it('restores saved "dark" from localStorage', () => {
    localStorage.setItem('fleetlift:theme', 'dark')
    mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    expect(result.current.theme).toBe('dark')
  })

  it('applies .dark class when theme is "dark"', () => {
    mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    act(() => result.current.setTheme('dark'))
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('removes .dark class when theme is "light"', () => {
    document.documentElement.classList.add('dark')
    mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    act(() => result.current.setTheme('light'))
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('applies .dark class in system mode when OS is dark', () => {
    mockMatchMedia(true)
    renderHook(() => useTheme())
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('does not apply .dark class in system mode when OS is light', () => {
    mockMatchMedia(false)
    renderHook(() => useTheme())
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('attaches matchMedia listener in system mode', () => {
    const mql = mockMatchMedia(false)
    renderHook(() => useTheme())
    expect(mql.addEventListener).toHaveBeenCalledWith('change', expect.any(Function))
  })

  it('detaches matchMedia listener when switching away from system', () => {
    const mql = mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    act(() => result.current.setTheme('light'))
    expect(mql.removeEventListener).toHaveBeenCalledWith('change', expect.any(Function))
  })

  it('re-attaches matchMedia listener when switching back to system', () => {
    const mql = mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    act(() => result.current.setTheme('light'))
    act(() => result.current.setTheme('system'))
    // addEventListener called twice: initial mount + re-attach
    expect(mql.addEventListener).toHaveBeenCalledTimes(2)
  })

  it('detaches listener on unmount', () => {
    const mql = mockMatchMedia(false)
    const { unmount } = renderHook(() => useTheme())
    unmount()
    expect(mql.removeEventListener).toHaveBeenCalledWith('change', expect.any(Function))
  })

  it('updates .dark class when OS theme changes while in system mode', () => {
    const mql = mockMatchMedia(false)
    renderHook(() => useTheme())
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    act(() => mql._fire(true))
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('persists theme to localStorage on change', () => {
    mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    act(() => result.current.setTheme('dark'))
    expect(localStorage.getItem('fleetlift:theme')).toBe('dark')
  })

  it('does not throw when localStorage.getItem throws', () => {
    mockMatchMedia(false)
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('SecurityError') })
    expect(() => renderHook(() => useTheme())).not.toThrow()
  })

  it('does not throw when localStorage.setItem throws', () => {
    mockMatchMedia(false)
    const { result } = renderHook(() => useTheme())
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('SecurityError') })
    expect(() => act(() => result.current.setTheme('dark'))).not.toThrow()
  })
})
