import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RunListPage } from './RunList'
import { vi } from 'vitest'

vi.mock('@/api/client', () => ({
  api: {
    listRuns: vi.fn().mockResolvedValue({
      items: [
        {
          id: 'abc12345-0000-0000-0000-000000000000',
          workflow_title: 'Test Workflow',
          status: 'complete',
          started_at: '2026-03-16T10:00:00Z',
          completed_at: '2026-03-16T10:05:30Z',
          created_at: '2026-03-16T10:00:00Z',
        },
      ],
    }),
  },
}))

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  )
}

test('shows Duration column header', async () => {
  render(<RunListPage />, { wrapper })
  expect(await screen.findByText('Duration')).toBeInTheDocument()
})

test('shows formatted duration for completed run', async () => {
  render(<RunListPage />, { wrapper })
  expect(await screen.findByText('5m 30s')).toBeInTheDocument()
})
