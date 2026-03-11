import { useState } from 'react'
import type { StepRun } from '@/api/types'
import { Button } from './ui/button'

interface HITLPanelProps {
  stepRun: StepRun
  onApprove: () => void
  onReject: () => void
  onSteer: (prompt: string) => void
}

export function HITLPanel({ stepRun, onApprove, onReject, onSteer }: HITLPanelProps) {
  const [steerPrompt, setSteerPrompt] = useState('')
  const [loading, setLoading] = useState(false)

  if (stepRun.status !== 'awaiting_input') {
    return null
  }

  const handleApprove = async () => {
    setLoading(true)
    try { onApprove() } finally { setLoading(false) }
  }

  const handleReject = async () => {
    setLoading(true)
    try { onReject() } finally { setLoading(false) }
  }

  const handleSteer = () => {
    if (!steerPrompt.trim()) return
    onSteer(steerPrompt)
    setSteerPrompt('')
  }

  return (
    <div className="rounded-lg border border-yellow-500/30 bg-yellow-500/5 p-4 space-y-4">
      <div className="flex items-center gap-2">
        <div className="h-2 w-2 rounded-full bg-yellow-500 animate-pulse" />
        <h3 className="text-sm font-semibold">Awaiting Your Input</h3>
      </div>

      <p className="text-sm text-muted-foreground">
        Step "{stepRun.step_title || stepRun.step_id}" is waiting for approval.
        Review the output above, then approve, reject, or provide steering instructions.
      </p>

      <div className="flex gap-2">
        <Button onClick={handleApprove} disabled={loading} size="sm">
          Approve
        </Button>
        <Button onClick={handleReject} disabled={loading} variant="destructive" size="sm">
          Reject
        </Button>
      </div>

      <div className="space-y-2">
        <label className="text-xs font-medium text-muted-foreground">Or steer with new instructions:</label>
        <textarea
          className="w-full rounded-md border bg-background p-2 text-sm min-h-[80px] resize-y"
          placeholder="Provide additional instructions..."
          value={steerPrompt}
          onChange={(e) => setSteerPrompt(e.target.value)}
        />
        <Button onClick={handleSteer} disabled={loading || !steerPrompt.trim()} variant="secondary" size="sm">
          Send Steering
        </Button>
      </div>
    </div>
  )
}
