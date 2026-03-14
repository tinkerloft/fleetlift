import { describe, it, expect } from 'vitest'
import { render } from '@testing-library/react'
import { DiffViewer } from '../DiffViewer'

const SAMPLE_DIFF = `diff --git a/foo.ts b/foo.ts
--- a/foo.ts
+++ b/foo.ts
@@ -1,3 +1,3 @@
 const x = 1
-const y = 2
+const y = 3`

describe('DiffViewer', () => {
  it('renders all diff lines', () => {
    const { container } = render(<DiffViewer diff={SAMPLE_DIFF} />)
    // Each line gets class px-3 whitespace-pre
    const lines = container.querySelectorAll('.whitespace-pre')
    expect(lines.length).toBe(SAMPLE_DIFF.split('\n').length)
  })

  it('applies green highlight to added lines', () => {
    const { container } = render(<DiffViewer diff={SAMPLE_DIFF} />)
    const addedLine = Array.from(container.querySelectorAll('div')).find(
      el => el.textContent === '+const y = 3'
    )
    expect(addedLine?.className).toContain('text-green-700')
  })

  it('applies red highlight to removed lines', () => {
    const { container } = render(<DiffViewer diff={SAMPLE_DIFF} />)
    const removedLine = Array.from(container.querySelectorAll('div')).find(
      el => el.textContent === '-const y = 2'
    )
    expect(removedLine?.className).toContain('text-red-700')
  })

  it('applies blue to @@ hunk header', () => {
    const { container } = render(<DiffViewer diff={SAMPLE_DIFF} />)
    const hunkLine = Array.from(container.querySelectorAll('div')).find(
      el => el.textContent?.startsWith('@@')
    )
    expect(hunkLine?.className).toContain('text-blue-600')
  })

  it('does not highlight +++ line as added', () => {
    const { container } = render(<DiffViewer diff={SAMPLE_DIFF} />)
    const plusPlusLine = Array.from(container.querySelectorAll('div')).find(
      el => el.textContent === '+++ b/foo.ts'
    )
    // +++ lines should not get green coloring
    expect(plusPlusLine?.className ?? '').not.toContain('text-green-700')
  })

  it('renders empty diff without crashing', () => {
    const { container } = render(<DiffViewer diff="" />)
    // split('') gives [''], so one line div is rendered
    const lines = container.querySelectorAll('.whitespace-pre')
    expect(lines.length).toBe(1)
  })
})
