import { useState, useEffect } from 'react'
import { formatDuration } from './format'

/**
 * Returns a ticking formatted duration string.
 * If endTime is provided, shows static completed duration.
 * If only startTime, ticks every second.
 */
export function useLiveDuration(startTime?: string, endTime?: string): string | null {
  const [now, setNow] = useState(Date.now())

  useEffect(() => {
    if (!startTime || endTime) return
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [startTime, endTime])

  if (!startTime) return null

  const start = new Date(startTime).getTime()
  const end = endTime ? new Date(endTime).getTime() : now
  return formatDuration(end - start)
}
