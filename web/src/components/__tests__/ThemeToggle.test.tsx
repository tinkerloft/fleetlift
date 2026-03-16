import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { TooltipProvider } from '@/components/ui/tooltip'
import { ThemeToggle } from '../ThemeToggle'

function Wrapper({ children }: { children: React.ReactNode }) {
  return <TooltipProvider>{children}</TooltipProvider>
}

// Stub useTheme so tests control state directly
let mockTheme: 'system' | 'light' | 'dark' = 'system'
const mockSetTheme = vi.fn((next: 'system' | 'light' | 'dark') => { mockTheme = next })

vi.mock('@/hooks/useTheme', () => ({
  useTheme: () => ({ theme: mockTheme, setTheme: mockSetTheme }),
}))

beforeEach(() => {
  mockTheme = 'system'
  mockSetTheme.mockClear()
  // matchMedia stub (required by jsdom)
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })),
  })
})

describe('ThemeToggle', () => {
  it('renders the system icon when theme is "system"', () => {
    render(<ThemeToggle />, { wrapper: Wrapper })
    expect(screen.getByTitle('Theme: System')).toBeTruthy()
  })

  it('renders the light icon when theme is "light"', () => {
    mockTheme = 'light'
    render(<ThemeToggle />, { wrapper: Wrapper })
    expect(screen.getByTitle('Theme: Light')).toBeTruthy()
  })

  it('renders the dark icon when theme is "dark"', () => {
    mockTheme = 'dark'
    render(<ThemeToggle />, { wrapper: Wrapper })
    expect(screen.getByTitle('Theme: Dark')).toBeTruthy()
  })

  it('cycles system → light on click', () => {
    render(<ThemeToggle />, { wrapper: Wrapper })
    fireEvent.click(screen.getByRole('button'))
    expect(mockSetTheme).toHaveBeenCalledWith('light')
  })

  it('cycles light → dark on click', () => {
    mockTheme = 'light'
    render(<ThemeToggle />, { wrapper: Wrapper })
    fireEvent.click(screen.getByRole('button'))
    expect(mockSetTheme).toHaveBeenCalledWith('dark')
  })

  it('cycles dark → system on click', () => {
    mockTheme = 'dark'
    render(<ThemeToggle />, { wrapper: Wrapper })
    fireEvent.click(screen.getByRole('button'))
    expect(mockSetTheme).toHaveBeenCalledWith('system')
  })
})
