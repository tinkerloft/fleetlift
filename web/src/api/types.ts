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
