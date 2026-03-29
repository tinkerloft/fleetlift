import { Shield, Bug, GitBranch, Search, Tag, Terminal } from 'lucide-react'
import type React from 'react'

export type CategoryColor = 'violet' | 'blue' | 'teal' | 'amber' | 'red' | 'gray'

export interface WorkflowCategoryInfo {
  color: CategoryColor
  icon: string // Lucide icon name
}

const TAG_MAP: [string[], CategoryColor, string][] = [
  [['audit', 'security'], 'violet', 'Shield'],
  [['bug-fix', 'incident', 'incident-response'], 'red', 'Bug'],
  [['migration', 'fleet-transform', 'dependency', 'dependency-update', 'transform'], 'blue', 'GitBranch'],
  [['research', 'fleet-research', 'pr-review', 'review'], 'teal', 'Search'],
  [['triage', 'ops'], 'amber', 'Tag'],
]

export function workflowCategory(tags: string[]): WorkflowCategoryInfo {
  for (const [keywords, color, icon] of TAG_MAP) {
    if (tags.some(t => keywords.includes(t))) {
      return { color, icon }
    }
  }
  return { color: 'gray', icon: 'Terminal' }
}

export const WORKFLOW_ICON_MAP: Record<string, React.FC<{ className?: string }>> = {
  Shield, Bug, GitBranch, Search, Tag, Terminal,
}

/** Tailwind classes for category accent colors */
export const CATEGORY_STYLES: Record<CategoryColor, { border: string; bg: string; text: string; iconBg: string }> = {
  violet: { border: 'border-t-violet-500', bg: 'bg-violet-500/10', text: 'text-violet-600', iconBg: 'bg-violet-500/10' },
  blue:   { border: 'border-t-blue-500',   bg: 'bg-blue-500/10',   text: 'text-blue-600',   iconBg: 'bg-blue-500/10' },
  teal:   { border: 'border-t-teal-500',   bg: 'bg-teal-500/10',   text: 'text-teal-600',   iconBg: 'bg-teal-500/10' },
  amber:  { border: 'border-t-amber-500',  bg: 'bg-amber-500/10',  text: 'text-amber-600',  iconBg: 'bg-amber-500/10' },
  red:    { border: 'border-t-red-500',     bg: 'bg-red-500/10',    text: 'text-red-600',    iconBg: 'bg-red-500/10' },
  gray:   { border: 'border-t-gray-400',   bg: 'bg-gray-500/10',   text: 'text-gray-500',   iconBg: 'bg-gray-500/10' },
}
