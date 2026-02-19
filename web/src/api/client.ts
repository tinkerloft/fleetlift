import type {
  TaskSummary, DiffOutput, VerifierOutput,
  SteeringState, ExecutionProgress,
} from './types'

const BASE = '/api/v1'

async function get<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`)
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error ?? res.statusText)
  }
  return res.json()
}

async function post<T>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: 'POST',
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error ?? res.statusText)
  }
  return res.json()
}

export const api = {
  listTasks: (status?: string) =>
    get<{ tasks: TaskSummary[] }>(`/tasks${status ? `?status=${status}` : ''}`),
  getInbox: () => get<{ items: TaskSummary[] }>('/tasks/inbox'),
  getTask:  (id: string) => get<TaskSummary>(`/tasks/${id}`),
  getDiff:  (id: string) => get<{ diffs: DiffOutput[] }>(`/tasks/${id}/diff`),
  getLogs:  (id: string) => get<{ logs: VerifierOutput[] }>(`/tasks/${id}/logs`),
  getSteering: (id: string) => get<SteeringState>(`/tasks/${id}/steering`),
  getProgress: (id: string) => get<ExecutionProgress>(`/tasks/${id}/progress`),

  approve: (id: string) => post<{ status: string }>(`/tasks/${id}/approve`),
  reject:  (id: string) => post<{ status: string }>(`/tasks/${id}/reject`),
  cancel:  (id: string) => post<{ status: string }>(`/tasks/${id}/cancel`),
  steer:   (id: string, prompt: string) =>
    post<{ status: string }>(`/tasks/${id}/steer`, { prompt }),
  continue: (id: string, skipRemaining: boolean) =>
    post<{ status: string }>(`/tasks/${id}/continue`, { skip_remaining: skipRemaining }),
}

/** Subscribe to live status updates via SSE. Returns an unsubscribe function. */
export function subscribeToTask(
  id: string,
  onStatus: (status: string) => void,
  onError?: (e: Event) => void,
): () => void {
  const es = new EventSource(`${BASE}/tasks/${id}/events`)
  es.addEventListener('status', (e) => {
    const data = JSON.parse((e as MessageEvent).data)
    onStatus(data.status)
  })
  if (onError) es.onerror = onError
  return () => es.close()
}
