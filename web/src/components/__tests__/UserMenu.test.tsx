import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { UserMenu } from '../UserMenu'

vi.mock('@/api/client', () => ({
  api: {
    getMe: vi.fn().mockResolvedValue({
      user_id: 'u1',
      name: 'Jane Doe',
      email: 'jane@example.com',
      teams: [{ id: 't1', name: 'Acme Corp', slug: 'acme', role: 'admin' }],
      team_roles: { t1: 'admin' },
      platform_admin: false,
    }),
  },
}))

function Wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  )
}

describe('UserMenu', () => {
  it('renders trigger button while loading', () => {
    render(<UserMenu />, { wrapper: Wrapper })
    // While loading, displayName = 'User', initials = 'U'
    expect(screen.getByText('U')).toBeTruthy()
    expect(screen.getByText('User')).toBeTruthy()
  })

  it('shows user initials after data loads', async () => {
    render(<UserMenu />, { wrapper: Wrapper })
    // After resolve: Jane Doe → 'JD'
    expect(await screen.findByText('JD')).toBeTruthy()
  })

  it('shows user display name after data loads', async () => {
    render(<UserMenu />, { wrapper: Wrapper })
    expect(await screen.findByText('Jane Doe')).toBeTruthy()
  })

  it('shows team name after data loads', async () => {
    render(<UserMenu />, { wrapper: Wrapper })
    expect(await screen.findByText('Acme Corp')).toBeTruthy()
  })
})
