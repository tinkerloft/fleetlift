import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ThemeToggle } from '../ThemeToggle'

let mockTheme: 'system' | 'light' | 'dark' = 'system'
const mockSetTheme = vi.fn((next: 'system' | 'light' | 'dark') => { mockTheme = next })

vi.mock('@/hooks/useTheme', () => ({
  useTheme: () => ({ theme: mockTheme, setTheme: mockSetTheme }),
}))

beforeEach(() => {
  mockTheme = 'system'
  mockSetTheme.mockClear()
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })),
  })
})

describe('ThemeToggle', () => {
  // Trigger label tests — no need to open dropdown
  it('shows "System" label in trigger when theme is "system"', () => {
    render(<ThemeToggle />)
    expect(screen.getByText('System')).toBeTruthy()
  })

  it('shows "Light" label in trigger when theme is "light"', () => {
    mockTheme = 'light'
    render(<ThemeToggle />)
    expect(screen.getByText('Light')).toBeTruthy()
  })

  it('shows "Dark" label in trigger when theme is "dark"', () => {
    mockTheme = 'dark'
    render(<ThemeToggle />)
    expect(screen.getByText('Dark')).toBeTruthy()
  })

  // Trigger icon tests — button contains 2 SVGs: theme icon + ChevronUp
  it('renders theme icon and ChevronUp in trigger when theme is "system"', () => {
    render(<ThemeToggle />)
    expect(screen.getByRole('button').querySelectorAll('svg').length).toBe(2)
  })

  it('renders theme icon and ChevronUp in trigger when theme is "light"', () => {
    mockTheme = 'light'
    render(<ThemeToggle />)
    expect(screen.getByRole('button').querySelectorAll('svg').length).toBe(2)
  })

  it('renders theme icon and ChevronUp in trigger when theme is "dark"', () => {
    mockTheme = 'dark'
    render(<ThemeToggle />)
    expect(screen.getByRole('button').querySelectorAll('svg').length).toBe(2)
  })

  // Dropdown item interaction — open menu then click items
  // Radix DropdownMenu requires full pointer sequence to open in jsdom
  function openDropdown() {
    const trigger = screen.getByRole('button')
    fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false, pointerType: 'mouse', pointerId: 1 })
    fireEvent.pointerUp(trigger)
    fireEvent.click(trigger)
  }

  it('calls setTheme("system") when System item is clicked', () => {
    render(<ThemeToggle />)
    openDropdown()
    fireEvent.click(screen.getByRole('menuitem', { name: /system/i }))
    expect(mockSetTheme).toHaveBeenCalledWith('system')
  })

  it('calls setTheme("light") when Light item is clicked', () => {
    render(<ThemeToggle />)
    openDropdown()
    fireEvent.click(screen.getByRole('menuitem', { name: /light/i }))
    expect(mockSetTheme).toHaveBeenCalledWith('light')
  })

  it('calls setTheme("dark") when Dark item is clicked', () => {
    render(<ThemeToggle />)
    openDropdown()
    fireEvent.click(screen.getByRole('menuitem', { name: /dark/i }))
    expect(mockSetTheme).toHaveBeenCalledWith('dark')
  })

  it('shows check icon on the active theme item only', () => {
    mockTheme = 'dark'
    render(<ThemeToggle />)
    openDropdown()
    // Active item (Dark) has 2 SVGs: theme icon + Check icon
    const darkItem = screen.getByRole('menuitem', { name: /dark/i })
    expect(darkItem.querySelectorAll('svg').length).toBe(2)
    // Inactive items have 1 SVG: theme icon only
    const systemItem = screen.getByRole('menuitem', { name: /system/i })
    expect(systemItem.querySelectorAll('svg').length).toBe(1)
    const lightItem = screen.getByRole('menuitem', { name: /light/i })
    expect(lightItem.querySelectorAll('svg').length).toBe(1)
  })
})
