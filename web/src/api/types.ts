export type TaskStatus =
  | 'pending' | 'provisioning' | 'cloning' | 'running'
  | 'awaiting_approval' | 'creating_prs' | 'completed' | 'failed' | 'cancelled'

export type InboxType = 'awaiting_approval' | 'paused' | 'steering_requested' | 'completed_review'

export interface TaskSummary {
  workflow_id: string
  status: TaskStatus
  start_time: string
  inbox_type?: InboxType
  is_paused?: boolean
}

export interface FileDiff {
  path: string
  status: 'modified' | 'added' | 'deleted'
  additions: number
  deletions: number
  diff: string
}

export interface DiffOutput {
  repository: string
  files: FileDiff[]
  summary: string
  total_lines: number
  truncated: boolean
}

export interface VerifierOutput {
  verifier: string
  exit_code: number
  stdout: string
  stderr: string
  success: boolean
}

export interface SteeringIteration {
  iteration_number: number
  prompt: string
  timestamp: string
  files_modified?: string[]
  output?: string
}

export interface SteeringState {
  current_iteration: number
  max_iterations: number
  history: SteeringIteration[]
}

export interface ExecutionProgress {
  total_groups: number
  completed_groups: number
  failed_groups: number
  failure_percent: number
  is_paused: boolean
  paused_reason?: string
  failed_group_names?: string[]
}

// Result types

export interface PullRequest {
  repo_name: string
  pr_url: string
  pr_number: number
  branch_name: string
  title: string
}

export interface ReportOutput {
  frontmatter?: Record<string, unknown>
  body?: string
  raw: string
  error?: string
  validation_errors?: string[]
}

export interface RepositoryResult {
  repository: string
  status: string
  files_modified?: string[]
  pull_request?: PullRequest
  report?: ReportOutput
  error?: string
}

export interface GroupResult {
  group_name: string
  status: string
  repositories?: RepositoryResult[]
  error?: string
}

export interface TaskResult {
  task_id: string
  status: TaskStatus
  mode?: string
  repositories?: RepositoryResult[]
  groups?: GroupResult[]
  started_at?: string
  completed_at?: string
  error?: string
  duration_seconds?: number
  pull_requests?: PullRequest[]
}

export interface AppConfig {
  temporal_ui_url: string
}

// AI Chat / Create types

export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
}

export interface Template {
  name: string
  description: string
  content?: string
}
