import type {
  WorkflowTemplate, Run, StepRunLog,
  InboxItem, Artifact, ListResponse, RunStatusUpdate,
} from './types'

const BASE = '/api'

function authHeaders(): Record<string, string> {
  const token = localStorage.getItem('token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}

async function get<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`, { headers: authHeaders() })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error ?? res.statusText)
  }
  return res.json()
}

async function post<T>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeaders() },
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error ?? res.statusText)
  }
  return res.json()
}

export async function del<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`, { method: 'DELETE', headers: authHeaders() })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error ?? res.statusText)
  }
  return res.json()
}

export const api = {
  // Workflows
  listWorkflows: () => get<ListResponse<WorkflowTemplate>>('/workflows'),
  getWorkflow: (id: string) => get<WorkflowTemplate>(`/workflows/${id}`),

  // Runs
  createRun: (workflowId: string, parameters: Record<string, unknown>) =>
    post<Run>('/runs', { workflow_id: workflowId, parameters }),
  listRuns: () => get<ListResponse<Run>>('/runs'),
  getRun: (id: string) => get<Run>(`/runs/${id}`),
  getRunLogs: (id: string) => get<ListResponse<StepRunLog>>(`/runs/${id}/logs`),
  getRunDiff: (id: string) => get<{ diff: string }>(`/runs/${id}/diff`),
  getRunOutput: (id: string) => get<Record<string, unknown>>(`/runs/${id}/output`),
  approveRun: (id: string) => post<{ status: string }>(`/runs/${id}/approve`),
  rejectRun: (id: string) => post<{ status: string }>(`/runs/${id}/reject`),
  steerRun: (id: string, prompt: string) =>
    post<{ status: string }>(`/runs/${id}/steer`, { prompt }),
  cancelRun: (id: string) => post<{ status: string }>(`/runs/${id}/cancel`),

  // Inbox
  listInbox: () => get<ListResponse<InboxItem>>('/inbox'),
  markInboxRead: (id: string) => post<{ status: string }>(`/inbox/${id}/read`),

  // Reports
  listReports: () => get<ListResponse<Run>>('/reports'),
  getReport: (runId: string) => get<Run>(`/reports/${runId}`),
  getReportArtifacts: (runId: string) => get<ListResponse<Artifact>>(`/reports/${runId}/artifacts`),
}

/** Subscribe to live run updates via SSE. Returns an unsubscribe function. */
export function subscribeToRun(
  runId: string,
  onStatus: (update: RunStatusUpdate) => void,
  onLog: (log: StepRunLog) => void,
  onError?: (e: Event) => void,
): () => void {
  const token = localStorage.getItem('token')
  const url = `${BASE}/runs/${runId}/events${token ? `?token=${token}` : ''}`
  const es = new EventSource(url)

  es.addEventListener('status', (e) => {
    const data = JSON.parse((e as MessageEvent).data)
    onStatus(data)
  })

  es.onmessage = (e) => {
    const data = JSON.parse(e.data)
    if (data.step_run_id) {
      onLog(data as StepRunLog)
    }
  }

  if (onError) es.onerror = onError
  return () => es.close()
}
