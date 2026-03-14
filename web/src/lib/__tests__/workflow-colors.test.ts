import { describe, it, expect } from 'vitest'
import { workflowCategory } from '../workflow-colors'

describe('workflowCategory', () => {
  it('maps audit tag to violet', () => {
    const cat = workflowCategory(['audit', 'security'])
    expect(cat.color).toBe('violet')
    expect(cat.icon).toBe('Shield')
  })
  it('maps bug-fix tag to red', () => {
    const cat = workflowCategory(['bug-fix'])
    expect(cat.color).toBe('red')
    expect(cat.icon).toBe('Bug')
  })
  it('maps migration tag to blue', () => {
    const cat = workflowCategory(['migration', 'fleet'])
    expect(cat.color).toBe('blue')
    expect(cat.icon).toBe('GitBranch')
  })
  it('maps research tag to teal', () => {
    const cat = workflowCategory(['fleet-research'])
    expect(cat.color).toBe('teal')
    expect(cat.icon).toBe('Search')
  })
  it('maps triage tag to amber', () => {
    const cat = workflowCategory(['triage'])
    expect(cat.color).toBe('amber')
    expect(cat.icon).toBe('Tag')
  })
  it('falls back to gray for unknown', () => {
    const cat = workflowCategory(['unknown-thing'])
    expect(cat.color).toBe('gray')
    expect(cat.icon).toBe('Terminal')
  })
  it('handles empty tags', () => {
    const cat = workflowCategory([])
    expect(cat.color).toBe('gray')
  })
})
