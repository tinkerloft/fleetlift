import { useState, useEffect } from 'react'

interface KnowledgeItem {
  id: string
  type: string
  summary: string
  details?: string
  tags?: string[]
  confidence: number
  status: 'pending' | 'approved' | 'rejected'
  workflow_template_id?: string
  created_at: string
}

export function KnowledgePage() {
  const [items, setItems] = useState<KnowledgeItem[]>([])
  const [statusFilter, setStatusFilter] = useState('pending')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setError(null)
    fetch(`/api/knowledge?status=${statusFilter}`, {
      headers: { Authorization: `Bearer ${localStorage.getItem('token')}` },
    })
      .then(r => { if (!r.ok) throw new Error(`HTTP ${r.status}`); return r.json() })
      .then(data => setItems(data?.items ?? []))
      .catch(err => setError(String(err)))
      .finally(() => setLoading(false))
  }, [statusFilter])

  const updateStatus = async (id: string, status: string) => {
    try {
      const r = await fetch(`/api/knowledge/${id}`, {
        method: 'PATCH',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${localStorage.getItem('token')}`,
        },
        body: JSON.stringify({ status }),
      })
      if (!r.ok) throw new Error(`HTTP ${r.status}`)
      setItems(prev => prev.filter(i => i.id !== id))
    } catch (err) {
      setError(String(err))
    }
  }

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Knowledge Base</h1>
        <select
          className="border rounded px-3 py-1 text-sm"
          value={statusFilter}
          onChange={e => setStatusFilter(e.target.value)}
        >
          <option value="pending">Pending</option>
          <option value="approved">Approved</option>
          <option value="rejected">Rejected</option>
        </select>
      </div>

      {error && (
        <div className="rounded-md bg-red-500/10 border border-red-500/20 p-3 text-sm text-red-400">
          {error}
        </div>
      )}

      {loading && <p className="text-muted-foreground">Loading...</p>}
      {!loading && !error && items.length === 0 && <p className="text-muted-foreground">No items.</p>}

      {items.map(item => (
        <div key={item.id} className="border rounded-lg p-4 space-y-2">
          <div className="flex items-start justify-between gap-4">
            <div>
              <span className="text-xs font-mono uppercase text-muted-foreground mr-2">{item.type}</span>
              <span className="font-medium">{item.summary}</span>
              {item.details && <p className="text-sm text-muted-foreground mt-1">{item.details}</p>}
              {item.tags && item.tags.length > 0 && (
                <div className="flex gap-1 mt-1">
                  {item.tags.map(t => (
                    <span key={t} className="text-xs bg-muted px-2 py-0.5 rounded">{t}</span>
                  ))}
                </div>
              )}
            </div>
            {item.status === 'pending' && (
              <div className="flex gap-2 shrink-0">
                <button
                  onClick={() => updateStatus(item.id, 'approved')}
                  className="text-sm px-3 py-1 bg-green-600 text-white rounded hover:bg-green-700"
                >
                  Approve
                </button>
                <button
                  onClick={() => updateStatus(item.id, 'rejected')}
                  className="text-sm px-3 py-1 bg-red-600 text-white rounded hover:bg-red-700"
                >
                  Reject
                </button>
              </div>
            )}
          </div>
          <p className="text-xs text-muted-foreground">
            Confidence: {(item.confidence * 100).toFixed(0)}% · {new Date(item.created_at).toLocaleDateString()}
          </p>
        </div>
      ))}
    </div>
  )
}
