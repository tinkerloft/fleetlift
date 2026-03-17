import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RunListPage } from './RunList'
import { vi, expect, test } from 'vitest'
import type { Run } from '@/api/types'

const baseRun: Run = {
  id: 'abc12345-0000-0000-0000-000000000000',
  team_id: 'team-1',
  workflow_id: 'test-workflow',
  workflow_title: 'Test Workflow',
  parameters: {},
  status: 'complete',
  started_at: '2026-03-16T10:00:00Z',
  completed_at: '2026-03-16T10:05:30Z',
  created_at: '2026-03-16T10:00:00Z',
}

vi.mock('@/api/client', () => ({
  api: {
    listRuns: vi.fn().mockResolvedValue({
      items: [baseRun],
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

test('shows formatted cost for run with cost data', async () => {
  const { api } = await import('@/api/client')
  vi.mocked(api.listRuns).mockResolvedValueOnce({
    items: [{ ...baseRun, total_cost_usd: 0.05 }],
  })
  render(<RunListPage />, { wrapper })
  expect(await screen.findByText('$0.05')).toBeInTheDocument()
})
