import { useEffect, useRef, useState } from 'react'
import type { StepRunLog } from '@/api/types'

interface LogStreamProps {
  stepRunId: string
}

export function LogStream({ stepRunId }: LogStreamProps) {
  const [logs, setLogs] = useState<StepRunLog[]>([])
  const containerRef = useRef<HTMLDivElement>(null)
  const autoScrollRef = useRef(true)

  useEffect(() => {
    setLogs([])
    const token = localStorage.getItem('token')
    const url = `/api/runs/steps/${stepRunId}/logs${token ? `?token=${token}` : ''}`
    const es = new EventSource(url)

    es.onmessage = (e) => {
      const log = JSON.parse(e.data) as StepRunLog
      setLogs((prev) => [...prev, log])
    }

    es.onerror = () => {
      // SSE will reconnect automatically; ignore transient errors
    }

    return () => es.close()
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
      {logs.length === 0 && (
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
