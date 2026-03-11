/**
 * Simple YAML parser for workflow definitions.
 * Uses JSON.parse on the server response when available,
 * falls back to basic YAML key-value parsing for preview.
 */
export function parse(yamlStr: string): Record<string, unknown> {
  // Use js-yaml if available, otherwise try basic JSON parse
  // For the MVP, workflow defs come pre-parsed from the API.
  // This is a fallback for rendering the YAML body field.
  try {
    return JSON.parse(yamlStr)
  } catch {
    // Basic YAML-like parsing for simple cases
    const result: Record<string, unknown> = {}
    const lines = yamlStr.split('\n')
    for (const line of lines) {
      const match = line.match(/^(\w+):\s*(.+)$/)
      if (match) {
        result[match[1]] = match[2]
      }
    }
    return result
  }
}
