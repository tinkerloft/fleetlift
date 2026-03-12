import { useEffect, useRef, useState } from 'react'
import { post } from '@/api/client'
import type { StepRunLog } from '@/api/types'

interface LogStreamProps {
  stepRunId: string
}

export function LogStream({ stepRunId }: LogStreamProps) {
  const [logs, setLogs] = useState<StepRunLog[]>([])
  const [ticketError, setTicketError] = useState<string | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const autoScrollRef = useRef(true)

  useEffect(() => {
    setLogs([])
    setTicketError(null)
    let es: EventSource | undefined

    post<{ ticket: string }>(`/runs/steps/${stepRunId}/logs/ticket`, {}).then(({ ticket }) => {
      es = new EventSource(`/api/runs/steps/${stepRunId}/logs?ticket=${ticket}`)

      es.onmessage = (e) => {
        try {
          const log = JSON.parse(e.data) as StepRunLog
          setLogs((prev) => {
            const next = [...prev, log]
            return next.length > 2000 ? next.slice(-2000) : next
          })
        } catch { /* ignore malformed events */ }
      }

      es.onerror = () => {
        // SSE will reconnect automatically; ignore transient errors
      }
    }).catch((err: Error) => {
      setTicketError(err.message)
    })

    return () => es?.close()
  }, [stepRunId])

  useEffect(() => {
    if (autoScrollRef.current && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight
    }
  }, [logs])

  const handleScroll = () => {
    if (!containerRef.current) return
    const { scrollTop, scrollHeight, clientHeight } = containerRef.current
    autoScrollRef.current = scrollHeight - scrollTop - clientHeight < 50
  }

  return (
    <div
      ref={containerRef}
      onScroll={handleScroll}
      className="h-64 overflow-auto rounded-md bg-black/80 p-3 font-mono text-xs text-green-400"
    >
      {ticketError && (
        <span className="text-red-400">Failed to connect to log stream: {ticketError}</span>
      )}
      {!ticketError && logs.length === 0 && (
        <span className="text-muted-foreground">Waiting for logs...</span>
      )}
      {logs.map((log) => (
        <div key={log.id} className={log.stream === 'stderr' ? 'text-red-400' : ''}>
          {log.content}
        </div>
      ))}
    </div>
  )
}
