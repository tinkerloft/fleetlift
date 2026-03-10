import { useCallback, useEffect, useRef, useState } from 'react'
import CodeMirror from '@uiw/react-codemirror'
import { yaml } from '@codemirror/lang-yaml'
import { api } from '@/api/client'

interface Props {
  value: string
  onChange: (value: string) => void
  /** Optional: called with validation error message or null */
  onValidationChange?: (error: string | null) => void
}

export function TaskYamlEditor({ value, onChange, onValidationChange }: Props) {
  const [validationError, setValidationError] = useState<string | null>(null)
  const validateTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const validate = useCallback(async (v: string) => {
    if (!v.trim()) {
      setValidationError(null)
      onValidationChange?.(null)
      return
    }
    try {
      const result = await api.validateYAML(v)
      const err = result.valid ? null : (result.error ?? 'Invalid YAML')
      setValidationError(err)
      onValidationChange?.(err)
    } catch {
      // network error during validation — don't block the user
    }
  }, [onValidationChange])

  const handleChange = useCallback((v: string) => {
    onChange(v)
    if (validateTimer.current) clearTimeout(validateTimer.current)
    validateTimer.current = setTimeout(() => validate(v), 600)
  }, [onChange, validate])

  useEffect(() => {
    return () => {
      if (validateTimer.current) clearTimeout(validateTimer.current)
    }
  }, [])

  return (
    <div className="flex flex-col h-full">
      <div className="flex-1 overflow-auto text-xs font-mono">
        <CodeMirror
          value={value}
          onChange={handleChange}
          extensions={[yaml()]}
          basicSetup={{
            lineNumbers: true,
            foldGutter: true,
            highlightActiveLine: true,
            autocompletion: false,
          }}
          style={{ fontSize: '12px', height: '100%' }}
          height="100%"
        />
      </div>
      {validationError && (
        <div className="border-t px-3 py-2 text-xs text-destructive bg-destructive/5 flex-shrink-0">
          {validationError}
        </div>
      )}
    </div>
  )
}
