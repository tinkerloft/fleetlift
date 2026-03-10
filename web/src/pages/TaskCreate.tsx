import { useState, useRef, useEffect, useCallback } from 'react'
import { useNavigate, useSearchParams, Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api, streamChat } from '@/api/client'
import type { ChatMessage, Template } from '@/api/types'
import {
  Send, Sparkles, FileCode, Play, Loader2,
  ChevronRight, AlertCircle, CheckCircle2,
} from 'lucide-react'
import { cn, formatTemplateName, TRANSFORM_KEYWORDS } from '@/lib/utils'
import { TaskYamlEditor } from '@/components/TaskYamlEditor'

type ViewMode = 'chat' | 'templates'

export function TaskCreatePage() {
  const [viewMode, setViewMode] = useState<ViewMode>('chat')
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [input, setInput] = useState('')
  const [isStreaming, setIsStreaming] = useState(false)
  const [conversationId, setConversationId] = useState<string | null>(null)
  const [generatedYAML, setGeneratedYAML] = useState<string | null>(null)
  const [yamlWarning, setYamlWarning] = useState<string | null>(null)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [streamingContent, setStreamingContent] = useState('')
  const streamingContentRef = useRef('')
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const abortRef = useRef<AbortController | null>(null)
  const navigate = useNavigate()

  const { data: templatesData } = useQuery({
    queryKey: ['templates'],
    queryFn: () => api.listTemplates(),
  })

  const [searchParams] = useSearchParams()

  useEffect(() => {
    const templateName = searchParams.get('template')
    if (!templateName) return
    api.getTemplate(templateName)
      .then((t) => {
        if (t.content) {
          setGeneratedYAML(t.content)
          setYamlWarning(null)
        }
      })
      .catch(() => {
        // silently ignore unknown template param
      })
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []) // run once on mount

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [])

  useEffect(() => { scrollToBottom() }, [messages, streamingContent, scrollToBottom])
  useEffect(() => { inputRef.current?.focus() }, [])

  const sendMessage = async (text: string) => {
    if (!text.trim() || isStreaming) return

    const userMsg: ChatMessage = { role: 'user', content: text.trim() }
    setMessages(prev => [...prev, userMsg])
    setInput('')
    setIsStreaming(true)
    setStreamingContent('')
    setSubmitError(null)

    abortRef.current = new AbortController()

    await streamChat(
      text.trim(),
      conversationId,
      (id) => setConversationId(id),
      (delta) => setStreamingContent(prev => {
        const next = prev + delta
        streamingContentRef.current = next
        return next
      }),
      (data) => {
        const content = streamingContentRef.current
        streamingContentRef.current = ''
        setIsStreaming(false)
        setStreamingContent('')
        if (content) {
          const marker = '---YAML---'
          const markerIdx = content.indexOf(marker)
          const displayContent = markerIdx !== -1 ? content.substring(0, markerIdx).trim() : content
          setMessages(msgs => [...msgs, { role: 'assistant', content: displayContent || content }])
        }
        if (data.yaml) {
          setGeneratedYAML(data.yaml)
          setYamlWarning(data.yaml_warning ?? null)
        }
      },
      (error) => {
        setIsStreaming(false)
        setStreamingContent('')
        setMessages(prev => [...prev, { role: 'assistant', content: `Error: ${error}` }])
      },
      abortRef.current.signal,
    )
  }

  const handleSubmit = async () => {
    if (!generatedYAML || isSubmitting) return
    setIsSubmitting(true)
    setSubmitError(null)
    try {
      const { workflow_id } = await api.submitTask(generatedYAML)
      navigate(`/tasks/${workflow_id}`)
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : 'Failed to start workflow')
    } finally {
      setIsSubmitting(false)
    }
  }

  const handleTemplateSelect = async (tmpl: Template) => {
    try {
      const full = await api.getTemplate(tmpl.name)
      if (full.content) {
        setGeneratedYAML(full.content)
        setYamlWarning(null)
        setViewMode('chat')
      }
    } catch {
      // template load failed
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      sendMessage(input)
    }
  }

  const resetChat = () => {
    setMessages([])
    setConversationId(null)
    setGeneratedYAML(null)
    setYamlWarning(null)
    setStreamingContent('')
    setSubmitError(null)
  }

  return (
    <div className="flex h-[calc(100vh-3rem)] gap-0">
      {/* Chat panel */}
      <div className={cn(
        'flex flex-col',
        generatedYAML ? 'w-1/2 border-r' : 'w-full max-w-3xl mx-auto',
      )}>
        {/* Header */}
        <div className="flex items-center justify-between border-b px-4 py-3">
          <div className="flex items-center gap-3">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-violet-500 to-blue-600">
              <Sparkles className="h-4 w-4 text-white" />
            </div>
            <div>
              <h1 className="text-sm font-semibold">Create Task</h1>
              <p className="text-xs text-muted-foreground">AI-assisted task generation</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => setViewMode(viewMode === 'chat' ? 'templates' : 'chat')}
              className={cn(
                'flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
                viewMode === 'templates'
                  ? 'bg-primary text-primary-foreground'
                  : 'text-muted-foreground hover:bg-muted',
              )}
            >
              <FileCode className="h-3.5 w-3.5" />
              Templates
            </button>
            {messages.length > 0 && (
              <button
                onClick={resetChat}
                className="text-xs text-muted-foreground hover:text-foreground transition-colors"
              >
                New Chat
              </button>
            )}
          </div>
        </div>

        {viewMode === 'templates' ? (
          <TemplateGallery
            templates={templatesData?.templates ?? []}
            onSelect={handleTemplateSelect}
          />
        ) : (
          <>
            {/* Messages area */}
            <div className="flex-1 overflow-y-auto px-4 py-4 space-y-4">
              {messages.length === 0 && !isStreaming && (
                <EmptyState onSuggestion={sendMessage} />
              )}
              {messages.map((msg, i) => (
                <MessageBubble key={i} message={msg} />
              ))}
              {isStreaming && streamingContent && (
                <MessageBubble
                  message={{ role: 'assistant', content: streamingContent }}
                  isStreaming
                />
              )}
              {isStreaming && !streamingContent && (
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  <span>Thinking...</span>
                </div>
              )}
              <div ref={messagesEndRef} />
            </div>

            {/* Input area */}
            <div className="border-t px-4 py-3">
              <div className="relative flex items-end gap-2">
                <textarea
                  ref={inputRef}
                  value={input}
                  onChange={(e) => setInput(e.target.value)}
                  onKeyDown={handleKeyDown}
                  placeholder={messages.length === 0
                    ? 'Describe what you want to do across your repos...'
                    : 'Type your reply...'}
                  rows={1}
                  className="flex-1 resize-none rounded-lg border bg-background px-3 py-2.5 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                  disabled={isStreaming}
                />
                <button
                  onClick={() => sendMessage(input)}
                  disabled={!input.trim() || isStreaming}
                  className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50 transition-colors"
                >
                  <Send className="h-4 w-4" />
                </button>
              </div>
            </div>
          </>
        )}
      </div>

      {/* YAML Preview panel */}
      {generatedYAML && (
        <div className="flex w-1/2 flex-col">
          <div className="flex items-center justify-between border-b px-4 py-3">
            <div className="flex items-center gap-2">
              <FileCode className="h-4 w-4 text-muted-foreground" />
              <span className="text-sm font-medium">Generated Task YAML</span>
            </div>
            <div className="flex items-center gap-2">
              {yamlWarning && (
                <div className="flex items-center gap-1 text-xs text-warning">
                  <AlertCircle className="h-3.5 w-3.5" />
                  <span>{yamlWarning}</span>
                </div>
              )}
              <button
                onClick={() => { setGeneratedYAML(null); setYamlWarning(null) }}
                className="text-xs text-muted-foreground hover:text-foreground"
              >
                Dismiss
              </button>
            </div>
          </div>

          <div className="flex-1 overflow-hidden">
            <TaskYamlEditor
              value={generatedYAML}
              onChange={setGeneratedYAML}
            />
          </div>

          <div className="border-t px-4 py-3">
            {submitError && (
              <div className="mb-2 flex items-center gap-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive">
                <AlertCircle className="h-3.5 w-3.5" />
                {submitError}
              </div>
            )}
            <div className="flex items-center gap-2">
              <button
                onClick={() => {
                  // Copy to clipboard
                  navigator.clipboard.writeText(generatedYAML)
                }}
                className="rounded-md border px-3 py-2 text-xs font-medium text-muted-foreground hover:bg-muted transition-colors"
              >
                Copy YAML
              </button>
              <button
                onClick={handleSubmit}
                disabled={isSubmitting}
                className="ml-auto flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-xs font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50 transition-colors"
              >
                {isSubmitting ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Play className="h-3.5 w-3.5" />
                )}
                Run Task
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function EmptyState({ onSuggestion }: { onSuggestion: (text: string) => void }) {
  const suggestions = [
    { label: 'Upgrade dependencies', prompt: 'Upgrade all outdated npm dependencies across our frontend repos' },
    { label: 'Security audit', prompt: 'Run a security audit on our Go microservices looking for common vulnerabilities' },
    { label: 'Add structured logging', prompt: 'Replace fmt.Printf debug logs with slog structured logging in all Go services' },
    { label: 'Migrate API version', prompt: 'Migrate from v1 to v2 of our internal REST API across all consuming services' },
  ]

  return (
    <div className="flex flex-col items-center justify-center py-16 px-4">
      <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-br from-violet-500/10 to-blue-600/10 mb-5">
        <Sparkles className="h-7 w-7 text-violet-500" />
      </div>
      <h2 className="text-lg font-semibold mb-1">What would you like to do?</h2>
      <p className="text-sm text-muted-foreground mb-8 text-center max-w-md">
        Describe the task you want to run across your repositories.
        I'll help you create the right configuration.
      </p>
      <div className="grid grid-cols-2 gap-2 w-full max-w-lg">
        {suggestions.map((s) => (
          <button
            key={s.label}
            onClick={() => onSuggestion(s.prompt)}
            className="group flex items-center gap-2 rounded-lg border bg-card px-3 py-2.5 text-left text-xs hover:bg-muted/50 transition-colors"
          >
            <span className="flex-1 text-muted-foreground group-hover:text-foreground transition-colors">
              {s.label}
            </span>
            <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/50 group-hover:text-muted-foreground transition-colors" />
          </button>
        ))}
      </div>
    </div>
  )
}

function MessageBubble({ message, isStreaming }: { message: ChatMessage; isStreaming?: boolean }) {
  const isUser = message.role === 'user'
  return (
    <div className={cn('flex', isUser ? 'justify-end' : 'justify-start')}>
      <div className={cn(
        'max-w-[85%] rounded-xl px-4 py-2.5 text-sm leading-relaxed',
        isUser
          ? 'bg-primary text-primary-foreground'
          : 'bg-muted text-foreground',
      )}>
        <div className="whitespace-pre-wrap">{message.content}</div>
        {isStreaming && (
          <span className="inline-block w-1.5 h-4 ml-0.5 bg-foreground/60 animate-pulse" />
        )}
      </div>
    </div>
  )
}

function TemplateGallery({
  templates,
  onSelect,
}: {
  templates: Template[]
  onSelect: (t: Template) => void
}) {
  return (
    <div className="flex-1 overflow-y-auto p-4">
      <div className="mb-4 flex items-start justify-between">
        <div>
          <h2 className="text-sm font-medium">Task Templates</h2>
          <p className="text-xs text-muted-foreground mt-0.5">
            Start from a pre-built template and customize for your repos
          </p>
        </div>
        <Link to="/templates" className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors">
          Browse all
          <ChevronRight className="h-3 w-3" />
        </Link>
      </div>
      <div className="grid gap-3">
        {templates.map((t) => (
          <button
            key={t.name}
            onClick={() => onSelect(t)}
            className="group flex items-start gap-3 rounded-lg border bg-card p-4 text-left hover:bg-muted/50 transition-colors"
          >
            <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-muted">
              <FileCode className="h-4 w-4 text-muted-foreground" />
            </div>
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">{formatTemplateName(t.name)}</span>
                <InlineModeBadge description={t.description} />
                <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/50 group-hover:text-muted-foreground ml-auto transition-colors" />
              </div>
              <p className="text-xs text-muted-foreground mt-0.5 line-clamp-2">{t.description}</p>
            </div>
          </button>
        ))}
        {templates.length === 0 && (
          <div className="flex flex-col items-center py-12 text-muted-foreground">
            <CheckCircle2 className="h-8 w-8 mb-2 opacity-50" />
            <p className="text-sm">No templates available</p>
          </div>
        )}
      </div>
    </div>
  )
}

function InlineModeBadge({ description }: { description: string }) {
  const lower = description.toLowerCase()
  if (lower.includes('report')) {
    return (
      <span className="rounded px-1.5 py-0.5 text-[10px] font-medium bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
        report
      </span>
    )
  }
  if (TRANSFORM_KEYWORDS.some((k) => lower.includes(k))) {
    return (
      <span className="rounded px-1.5 py-0.5 text-[10px] font-medium bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400">
        transform
      </span>
    )
  }
  return null
}
