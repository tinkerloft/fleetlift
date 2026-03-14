import { describe, it, expect } from 'vitest'
import { formatDuration, formatTimeAgo } from '../format'

describe('formatDuration', () => {
  it('formats seconds only', () => {
    expect(formatDuration(42000)).toBe('42s')
  })
  it('formats minutes and seconds', () => {
    expect(formatDuration(154000)).toBe('2m 34s')
  })
  it('formats hours', () => {
    expect(formatDuration(3661000)).toBe('1h 1m')
  })
  it('returns 0s for zero', () => {
    expect(formatDuration(0)).toBe('0s')
  })
  it('returns 0s for negative', () => {
    expect(formatDuration(-1000)).toBe('0s')
  })
})

describe('formatTimeAgo', () => {
  it('returns "just now" for < 60s', () => {
    const now = Date.now()
    expect(formatTimeAgo(new Date(now - 30000).toISOString())).toBe('just now')
  })
  it('returns minutes ago', () => {
    const now = Date.now()
    expect(formatTimeAgo(new Date(now - 180000).toISOString())).toBe('3m ago')
  })
  it('returns hours ago', () => {
    const now = Date.now()
    expect(formatTimeAgo(new Date(now - 7200000).toISOString())).toBe('2h ago')
  })
})
