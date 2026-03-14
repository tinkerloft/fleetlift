import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { StatusBadge } from '../StatusBadge'

describe('StatusBadge', () => {
  it('renders single-word status', () => {
    render(<StatusBadge status="running" />)
    expect(screen.getByText('running')).toBeTruthy()
  })

  it('replaces all underscores with spaces', () => {
    render(<StatusBadge status="awaiting_input" />)
    expect(screen.getByText('awaiting input')).toBeTruthy()
  })

  it('shows pulse dot for running status', () => {
    const { container } = render(<StatusBadge status="running" />)
    // pulse dot uses animate-ping class
    expect(container.querySelector('.animate-ping')).toBeTruthy()
  })

  it('does not show pulse dot for terminal status', () => {
    const { container } = render(<StatusBadge status="complete" />)
    expect(container.querySelector('.animate-ping')).toBeNull()
  })

  it('renders failed status', () => {
    render(<StatusBadge status="failed" />)
    expect(screen.getByText('failed')).toBeTruthy()
  })

  it('renders skipped status', () => {
    render(<StatusBadge status="skipped" />)
    expect(screen.getByText('skipped')).toBeTruthy()
  })
})
