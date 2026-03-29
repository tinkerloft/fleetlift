import { useEffect, useMemo, useRef, useState } from 'react'
import type { StepRunLog } from '@/api/types'

interface LogStreamProps {
  stepRunId: string
}

function highlightMatch(text: string, query: string): React.ReactNode {
  if (!query) return text
  const idx = text.toLowerCase().indexOf(query.toLowerCase())
  if (idx === -1) return text
  return (
    <>
      {text.slice(0, idx)}
      <mark className="bg-yellow-500/30 text-yellow-200">{text.slice(idx, idx + query.length)}</mark>
      {text.slice(idx + query.length)}
    </>
  )
}

export function LogStream({ stepRunId }: LogStreamProps) {
  const [logs, setLogs] = useState<StepRunLog[]>([])
  const [connected, setConnected] = useState(false)
  const [search, setSearch] = useState('')
  const containerRef = useRef<HTMLDivElement>(null)
  const autoScrollRef = useRef(true)

  const filtered = useMemo(() => {
    if (!search) return logs
    const q = search.toLowerCase()
    return logs.filter((l) => l.content.toLowerCase().includes(q))
  }, [logs, search])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- intentional: reset logs when stepRunId changes before subscribing
    setLogs([])
    setConnected(false)
    const seen = new Set<number>()
    const es = new EventSource(`/api/runs/steps/${stepRunId}/logs`)

    es.onopen = () => setConnected(true)

    es.onmessage = (e) => {
      try {
        const log = JSON.parse(e.data) as StepRunLog
        if (seen.has(log.id)) return
        seen.add(log.id)
        setLogs((prev) => {
          const next = [...prev, log]
          return next.length > 2000 ? next.slice(-2000) : next
        })
      } catch { /* ignore malformed events */ }
    }

    // Server closes connection when step is terminal; prevent EventSource auto-reconnect.
    es.onerror = () => {
      setConnected(false)
      if (es.readyState === EventSource.CLOSED) return
      es.close()
    }

    return () => {
      es.close()
      setConnected(false)
    }
  }, [stepRunId])

  useEffect(() => {
    if (autoScrollRef.current && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight
    }
  }, [logs])

  const [showScrollBtn, setShowScrollBtn] = useState(false)

  const handleScroll = () => {
    if (!containerRef.current) return
    const { scrollTop, scrollHeight, clientHeight } = containerRef.current
    const atBottom = scrollHeight - scrollTop - clientHeight < 50
    autoScrollRef.current = atBottom
    setShowScrollBtn(!atBottom)
  }

  return (
    <div className="rounded-md overflow-hidden border border-gray-800">
      {/* Header */}
      <div className="flex items-center gap-2 bg-gray-900 px-3 py-1.5 border-b border-gray-800">
        <span className="text-[11px] text-gray-400">Logs</span>
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search logs..."
          className="flex-1 rounded border border-gray-700 bg-black/50 px-2 py-0.5 text-[11px] text-gray-300 placeholder:text-gray-600 focus:outline-none focus:border-gray-500"
        />
        {search && <span className="text-[11px] text-gray-600">{filtered.length}/{logs.length}</span>}
        {connected && logs.length > 0 && (
          <span className="flex items-center gap-1.5 text-[11px] text-gray-500">
            <span className="h-1.5 w-1.5 rounded-full bg-green-500 animate-pulse" />
            streaming
          </span>
        )}
        {showScrollBtn && (
          <button
            onClick={() => {
              autoScrollRef.current = true
              setShowScrollBtn(false)
              if (containerRef.current) containerRef.current.scrollTop = containerRef.current.scrollHeight
            }}
            className="text-[11px] text-gray-500 hover:text-gray-300 transition-colors"
          >
            ↓ scroll to bottom
          </button>
        )}
      </div>
      {/* Log content */}
      <div
        ref={containerRef}
        onScroll={handleScroll}
        data-testid="log-stream"
        className="h-56 overflow-auto bg-black/80 p-3 font-mono text-xs text-green-400"
      >
        {logs.length === 0 && (
          <span className="text-gray-600">Waiting for logs...</span>
        )}
        {search && filtered.length === 0 && logs.length > 0 && (
          <span className="text-gray-600">No matching logs</span>
        )}
        {filtered.map((log) => (
          <div key={log.id} className={log.stream === 'stderr' ? 'text-red-400' : ''}>
            {highlightMatch(log.content, search)}
          </div>
        ))}
      </div>
    </div>
  )
}
