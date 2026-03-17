import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useLiveDuration } from './use-live-duration'

describe('useLiveDuration', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns null when startTime is undefined', () => {
    const { result } = renderHook(() => useLiveDuration(undefined, undefined))
    expect(result.current).toBeNull()
  })

  it('returns a static formatted string when both startTime and endTime are provided', () => {
    const startTime = '2026-03-16T10:00:00.000Z'
    const endTime = '2026-03-16T10:05:30.000Z'
    const { result } = renderHook(() => useLiveDuration(startTime, endTime))
    expect(result.current).toBe('5m 30s')
  })

  it('does not start an interval when both startTime and endTime are provided', () => {
    vi.useFakeTimers()
    const startTime = '2026-03-16T10:00:00.000Z'
    const endTime = '2026-03-16T10:05:30.000Z'
    const { result } = renderHook(() => useLiveDuration(startTime, endTime))
    const initial = result.current
    act(() => { vi.advanceTimersByTime(5000) })
    expect(result.current).toBe(initial)
  })

  it('ticks (updates) when only startTime is provided', () => {
    vi.useFakeTimers()
    const now = Date.now()
    const startTime = new Date(now).toISOString()

    const { result } = renderHook(() => useLiveDuration(startTime, undefined))

    // Initially shows 0s
    expect(result.current).toBe('0s')

    // After 10 seconds, the duration should update
    act(() => { vi.advanceTimersByTime(10000) })
    expect(result.current).toBe('10s')

    // After another 50 seconds (total 60s), should show 1m
    act(() => { vi.advanceTimersByTime(50000) })
    expect(result.current).toBe('1m 0s')
  })
})
