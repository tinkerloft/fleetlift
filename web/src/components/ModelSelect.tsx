import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'

interface ModelSelectProps {
  value: string
  onChange: (v: string) => void
}

const STORAGE_KEY = 'fleetlift-preferred-model'

export function ModelSelect({ value, onChange }: ModelSelectProps) {
  const { data } = useQuery({
    queryKey: ['models'],
    queryFn: () => api.listModels(),
    staleTime: Infinity, // models list rarely changes
  })
  const models = data?.items ?? []

  return (
    <select
      value={value}
      onChange={(e) => {
        onChange(e.target.value)
        localStorage.setItem(STORAGE_KEY, e.target.value)
      }}
      className="rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm text-zinc-200"
    >
      <option value="">Default</option>
      {models.map((m) => (
        <option key={m.value} value={m.value}>{m.label}</option>
      ))}
    </select>
  )
}

export function getPreferredModel(): string {
  return localStorage.getItem(STORAGE_KEY) ?? ''
}
