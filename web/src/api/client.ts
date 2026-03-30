import type {
  WorkflowTemplate, Run, StepRunLog,
  InboxItem, Artifact, ListResponse, RunStatusUpdate,
  UserProfile, Credential, CreateRunResponse, ModelEntry,
  Preset, SavedRepo,
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

export function authHeaders(): Record<string, string> {
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

async function put<T = void>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: 'PUT',
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

export const api = {
  // Workflows
  listWorkflows: () => get<ListResponse<WorkflowTemplate>>('/workflows'),
  getWorkflow: (id: string) => get<WorkflowTemplate>(`/workflows/${id}`),

  // Runs
  createRun: (workflowId: string, parameters: Record<string, unknown>, model?: string) =>
    post<CreateRunResponse>('/runs', { workflow_id: workflowId, parameters, ...(model ? { model } : {}) }),
  listRuns: () => get<ListResponse<Run>>('/runs'),
  listMyRuns: (limit = 10) =>
    get<ListResponse<Run>>(`/runs?created_by=me&limit=${limit}`),
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
  resolveFanOut: (id: string, action: 'proceed' | 'terminate', stepId: string) =>
    post(`/runs/${id}/resolve-fanout`, { action, step_id: stepId }),

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

  // Artifacts
  getArtifactContentUrl: (id: string) => `${BASE}/artifacts/${id}/content`,

  // Models
  listModels: () =>
    get<ListResponse<ModelEntry>>('/models'),

  // Prompt
  improvePrompt: (prompt: string) =>
    post<{
      improved: string
      scores: Record<string, { rating: string; reason: string }>
      summary: string
    }>('/prompt/improve', { prompt }),

  // Presets
  listPresets: () => get<ListResponse<Preset>>('/presets'),
  createPreset: (title: string, prompt: string, scope: 'personal' | 'team') =>
    post<Preset>('/presets', { title, prompt, scope }),
  updatePreset: (id: string, updates: { title?: string; prompt?: string; scope?: string }) =>
    put<Preset>(`/presets/${id}`, updates),
  deletePreset: (id: string) => del(`/presets/${id}`),

  // Saved repos
  listSavedRepos: () => get<ListResponse<SavedRepo>>('/saved-repos'),
  createSavedRepo: (url: string, label?: string) =>
    post<SavedRepo>('/saved-repos', { url, label }),
  deleteSavedRepo: (id: string) => del(`/saved-repos/${id}`),
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
