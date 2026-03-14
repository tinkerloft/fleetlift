import { cn } from '@/lib/utils'

/** CSS-only syntax-highlighted diff viewer: +green, -red, @@blue */
export function DiffViewer({ diff }: { diff: string }) {
  const lines = diff.split('\n')
  return (
    <div className="overflow-auto max-h-80 rounded-md bg-muted/50 font-mono text-xs leading-relaxed">
      {lines.map((line, i) => (
        <div
          key={i}
          className={cn(
            'px-3 whitespace-pre',
            line.startsWith('+') && !line.startsWith('+++') && 'bg-green-500/10 text-green-700 dark:text-green-400',
            line.startsWith('-') && !line.startsWith('---') && 'bg-red-500/10 text-red-700 dark:text-red-400',
            line.startsWith('@@') && 'text-blue-600 dark:text-blue-400 font-medium',
            line.startsWith('diff ') && 'text-muted-foreground font-medium border-t border-border mt-1 pt-1',
          )}
        >
          {line}
        </div>
      ))}
    </div>
  )
}
