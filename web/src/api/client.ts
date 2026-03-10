import type {
  TaskSummary, DiffOutput, VerifierOutput,
  SteeringState, ExecutionProgress, TaskResult, AppConfig, AppHealth,
  Template, KnowledgeItem, KnowledgeFilters, CreateKnowledgeRequest,
  UpdateKnowledgeRequest, BulkAction,
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

async function put<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error ?? res.statusText)
  }
  return res.json()
}

async function del<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`, { method: 'DELETE' })
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
  getResult:   (id: string) => get<TaskResult>(`/tasks/${id}/result`),
  getConfig:   () => get<AppConfig>('/config'),
  getHealth:   () => get<AppHealth>('/health'),

  // Create & Templates
  submitTask: (yaml: string) =>
    post<{ workflow_id: string }>('/tasks', { yaml }),
  validateYAML: (yaml: string) =>
    post<{ valid: boolean; error?: string }>('/create/validate', { yaml }),
  listTemplates: () =>
    get<{ templates: Template[] }>('/templates'),
  getTemplate: (name: string) =>
    get<Template>(`/templates/${name}`),

  approve: (id: string) => post<{ status: string }>(`/tasks/${id}/approve`),
  reject:  (id: string) => post<{ status: string }>(`/tasks/${id}/reject`),
  cancel:  (id: string) => post<{ status: string }>(`/tasks/${id}/cancel`),
  steer:   (id: string, prompt: string) =>
    post<{ status: string }>(`/tasks/${id}/steer`, { prompt }),
  continue: (id: string, skipRemaining: boolean) =>
    post<{ status: string }>(`/tasks/${id}/continue`, { skip_remaining: skipRemaining }),

  // Retry
  getTaskYAML: (id: string) =>
    get<{ yaml: string }>(`/tasks/${id}/yaml`),
  retryTask: (id: string, yaml: string, failedOnly: boolean) =>
    post<{ workflow_id: string }>(`/tasks/${id}/retry`, { yaml, failed_only: failedOnly }),

  // Knowledge
  listKnowledge: (filters?: KnowledgeFilters) => {
    const params = new URLSearchParams()
    if (filters?.task_id) params.set('task_id', filters.task_id)
    if (filters?.type) params.set('type', filters.type)
    if (filters?.tag) params.set('tag', filters.tag)
    if (filters?.status) params.set('status', filters.status)
    const qs = params.toString()
    return get<{ items: KnowledgeItem[] }>(`/knowledge${qs ? `?${qs}` : ''}`)
  },
  getKnowledge: (id: string) => get<KnowledgeItem>(`/knowledge/${id}`),
  createKnowledge: (req: CreateKnowledgeRequest) =>
    post<KnowledgeItem>('/knowledge', req),
  updateKnowledge: (id: string, req: UpdateKnowledgeRequest) =>
    put<KnowledgeItem>(`/knowledge/${id}`, req),
  deleteKnowledge: (id: string) =>
    del<{ status: string }>(`/knowledge/${id}`),
  bulkKnowledge: (actions: BulkAction[]) =>
    post<{ status: string }>('/knowledge/bulk', { actions }),
  commitKnowledge: (repoPath: string) =>
    post<{ committed: number; repo_path: string }>('/knowledge/commit', { repo_path: repoPath }),
}

/** Stream a chat message for AI-assisted task creation via SSE. */
export async function streamChat(
  message: string,
  conversationId: string | null,
  onConversation: (id: string) => void,
  onDelta: (text: string) => void,
  onDone: (data: { done: boolean; yaml?: string; yaml_warning?: string }) => void,
  onError: (error: string) => void,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch(`${BASE}/create/chat`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ conversation_id: conversationId ?? '', message }),
    signal,
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    onError(err.error ?? res.statusText)
    return
  }

  const reader = res.body?.getReader()
  if (!reader) {
    onError('No response body')
    return
  }

  const decoder = new TextDecoder()
  let buffer = ''
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop() ?? ''
    let eventType = ''
    for (const line of lines) {
      if (line.startsWith('event: ')) {
        eventType = line.slice(7).trim()
      } else if (line.startsWith('data: ')) {
        const data = JSON.parse(line.slice(6))
        switch (eventType) {
          case 'conversation':
            onConversation(data.id)
            break
          case 'delta':
            onDelta(data.text)
            break
          case 'done':
            onDone(data)
            break
          case 'error':
            onError(data.error)
            break
        }
      }
    }
  }
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
