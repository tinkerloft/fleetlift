/** Simple syntax-colored JSON viewer */
export function JsonViewer({ data }: { data: unknown }) {
  const json = JSON.stringify(data, null, 2)
  // Colorize: keys, strings, numbers, booleans, nulls
  const html = json.replace(
    /("(?:\\.|[^"\\])*")\s*:/g, '<span class="text-blue-600 dark:text-blue-400">$1</span>:'
  ).replace(
    /:\s*("(?:\\.|[^"\\])*")/g, ': <span class="text-green-700 dark:text-green-400">$1</span>'
  ).replace(
    /:\s*(\d+(?:\.\d+)?)/g, ': <span class="text-amber-600 dark:text-amber-400">$1</span>'
  ).replace(
    /:\s*(true|false|null)/g, ': <span class="text-violet-600 dark:text-violet-400">$1</span>'
  )

  return (
    <pre
      className="overflow-auto max-h-60 rounded-md bg-muted/50 p-3 font-mono text-xs leading-relaxed"
      dangerouslySetInnerHTML={{ __html: html }}
    />
  )
}
