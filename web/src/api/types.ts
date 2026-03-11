// Run & Step statuses
export type RunStatus = 'pending' | 'running' | 'awaiting_input' | 'complete' | 'failed' | 'cancelled'
export type StepStatus = 'pending' | 'cloning' | 'running' | 'verifying' | 'awaiting_input' | 'complete' | 'failed' | 'skipped'

// Workflow templates
export interface WorkflowTemplate {
  id: string
  team_id: string
  slug: string
  title: string
  description: string
  tags: string[]
  yaml_body: string
  builtin: boolean
  created_at: string
  updated_at: string
}

export interface WorkflowDef {
  version: number
  id: string
  title: string
  description: string
  tags: string[]
  parameters: ParameterDef[]
  steps: StepDef[]
}

export interface ParameterDef {
  name: string
  type: string
  required: boolean
  default?: unknown
  description?: string
}

export interface StepDef {
  id: string
  title?: string
  depends_on?: string[]
  mode?: string
  execution?: { agent: string; prompt: string }
  approval_policy?: string
  optional?: boolean
  action?: { type: string; config: Record<string, unknown> }
}

// Runs
export interface Run {
  id: string
  team_id: string
  workflow_id: string
  workflow_title: string
  parameters: Record<string, unknown>
  status: RunStatus
  temporal_id?: string
  triggered_by?: string
  started_at?: string
  completed_at?: string
  created_at: string
  steps?: StepRun[]
}

export interface StepRun {
  id: string
  run_id: string
  step_id: string
  step_title: string
  status: StepStatus
  sandbox_id?: string
  sandbox_group?: string
  output?: Record<string, unknown>
  diff?: string
  pr_url?: string
  branch_name?: string
  error_message?: string
  started_at?: string
  completed_at?: string
  created_at: string
}

export interface StepRunLog {
  id: number
  step_run_id: string
  seq: number
  stream: string
  content: string
  ts: string
}

// Inbox
export interface InboxItem {
  id: string
  team_id: string
  run_id: string
  step_run_id?: string
  kind: string
  title: string
  summary?: string
  created_at: string
  read?: boolean
}

// Reports / Artifacts
export interface Artifact {
  id: string
  step_run_id: string
  name: string
  path: string
  size_bytes: number
  content_type: string
  storage: string
  created_at: string
}

// API responses
export interface ListResponse<T> {
  items: T[]
}

export interface RunStatusUpdate {
  run: Run
  steps: StepRun[]
}
