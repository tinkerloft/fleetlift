import type {
  WorkflowTemplate, Run, StepRunLog,
  InboxItem, Artifact, ListResponse, RunStatusUpdate,
  UserProfile, Credential,
} from './types'

const BASE = '/api'

// Server config (fetched once at startup)
let _config: { temporal_ui_url: string; dev_no_auth?: boolean } | null = null
export async function getConfig() {
  if (!_config) {
    const res = await fetch('/api/config')
    _config = await res.json()
  }
  return _config!
}

function authHeaders(): Record<string, string> {
  const token = localStorage.getItem('token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}

function formatApiError(err: Record<string, unknown>, fallback: string): string {
  const base = (err.error as string) ?? fallback
  const validationErrors = err.validation_errors as Array<{ field?: string; step_id?: string; message: string }> | undefined
  if (validationErrors?.length) {
    const details = validationErrors
      .map(e => [e.step_id, e.field, e.message].filter(Boolean).join(' › '))
      .join('; ')
    return `${base}: ${details}`
  }
  return base
}

async function get<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`, { headers: authHeaders() })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(formatApiError(err, res.statusText))
  }
  return res.json()
}

export async function post<T = void>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeaders() },
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(formatApiError(err, res.statusText))
  }
  if (res.status === 204 || res.headers.get('content-length') === '0') {
    return undefined as T
  }
  return res.json()
}

export async function del<T = void>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`, { method: 'DELETE', headers: authHeaders() })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error ?? res.statusText)
  }
  if (res.status === 204 || res.headers.get('content-length') === '0') {
    return undefined as T
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
  getRun: (id: string) => get<{ run: Run; steps: Run['steps']; workflow_yaml?: string }>(`/runs/${id}`)
    .then(({ run, steps, workflow_yaml }) => ({ ...run, steps, workflow_yaml })),
  getRunLogs: (id: string) => get<ListResponse<StepRunLog>>(`/runs/${id}/logs`),
  getRunDiff: (id: string) => get<{ step_id: string; diff: string }[]>(`/runs/${id}/diff`),
  getRunOutput: (id: string) => get<{ step_id: string; output: Record<string, unknown> }[]>(`/runs/${id}/output`),
  approveRun: (id: string) => post<{ status: string }>(`/runs/${id}/approve`),
  rejectRun: (id: string) => post<{ status: string }>(`/runs/${id}/reject`),
  steerRun: (id: string, prompt: string) =>
    post<{ status: string }>(`/runs/${id}/steer`, { prompt }),
  cancelRun: (id: string) => post<{ status: string }>(`/runs/${id}/cancel`),

  // Inbox
  listInbox: () => get<ListResponse<InboxItem>>('/inbox'),
  markInboxRead: (id: string) => post<{ status: string }>(`/inbox/${id}/read`),
  respondToInbox: (id: string, answer: string) => post(`/inbox/${id}/respond`, { answer }),

  // User
  getMe: () => get<UserProfile>('/me'),

  // Credentials
  listCredentials: () => get<Credential[]>('/credentials'),
  setCredential: (name: string, value: string) => post('/credentials', { name, value }),
  deleteCredential: (name: string) => del(`/credentials/${name}`),

  // System credentials (admin only)
  listSystemCredentials: () => get<Credential[]>('/system-credentials'),
  setSystemCredential: (name: string, value: string) => post('/system-credentials', { name, value }),
  deleteSystemCredential: (name: string) => del(`/system-credentials/${name}`),

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
  const es = new EventSource(`${BASE}/runs/${runId}/events`)

  es.addEventListener('status', (e) => {
    try {
      const data = JSON.parse((e as MessageEvent).data)
      onStatus(data)
    } catch { /* ignore malformed events */ }
  })

  es.onmessage = (e) => {
    try {
      const data = JSON.parse(e.data)
      if (data.step_run_id) {
        onLog(data as StepRunLog)
      }
    } catch { /* ignore malformed events */ }
  }

  es.onerror = (e) => {
    if (onError) onError(e)
    // Server closes connection when run is terminal; prevent auto-reconnect.
    if (es.readyState === EventSource.CLOSED) return
    es.close()
  }
  return () => es.close()
}
