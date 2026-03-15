import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { Credential } from '@/api/types'

function formatRelativeTime(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime()
  const minutes = Math.floor(diff / 60000)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  if (days < 7) return `${days}d ago`
  return `${Math.floor(days / 7)}w ago`
}

interface CredentialSectionProps {
  title: string
  queryKey: readonly string[]
  fetchFn: () => Promise<Credential[]>
  setFn: (name: string, value: string) => Promise<void>
  deleteFn: (name: string) => Promise<void>
}

function CredentialSection({ title, queryKey, fetchFn, setFn, deleteFn }: CredentialSectionProps) {
  const queryClient = useQueryClient()
  const [addForm, setAddForm] = useState({ isOpen: false, name: '', value: '' })
  const [pendingDelete, setPendingDelete] = useState<string | null>(null)
  const [formError, setFormError] = useState<string | null>(null)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  const { data: creds = [], isLoading, error } = useQuery({
    queryKey: [...queryKey],
    queryFn: fetchFn,
  })

  const setMutation = useMutation({
    mutationFn: ({ name, value }: { name: string; value: string }) => setFn(name, value),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...queryKey] })
      setAddForm({ isOpen: false, name: '', value: '' })
      setFormError(null)
    },
    onError: (err: Error) => setFormError(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (name: string) => deleteFn(name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...queryKey] })
      setPendingDelete(null)
      setDeleteError(null)
    },
    onError: (err: Error) => setDeleteError(err.message),
  })

  const handleSave = () => {
    if (!addForm.name || !addForm.value) {
      setFormError('Name and value are required')
      return
    }
    setMutation.mutate({ name: addForm.name, value: addForm.value })
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">{title}</h2>
        {!addForm.isOpen && (
          <button
            onClick={() => setAddForm({ isOpen: true, name: '', value: '' })}
            className="text-sm px-3 py-1.5 bg-primary text-primary-foreground rounded-md hover:bg-primary/90"
          >
            + Add
          </button>
        )}
      </div>

      {addForm.isOpen && (
        <div className="rounded-lg border border-primary/50 bg-primary/5 p-4 space-y-3">
          {formError && (
            <div className="rounded-md bg-red-500/10 border border-red-500/20 p-2 text-xs text-red-400">
              {formError}
            </div>
          )}
          <div className="flex gap-3">
            <input
              className="flex-1 rounded-md border bg-background px-3 py-1.5 text-sm font-mono"
              placeholder="CREDENTIAL_NAME"
              value={addForm.name}
              onChange={e => setAddForm(f => ({ ...f, name: e.target.value.toUpperCase() }))}
            />
            <input
              type="password"
              className="flex-1 rounded-md border bg-background px-3 py-1.5 text-sm"
              placeholder="Value"
              value={addForm.value}
              onChange={e => setAddForm(f => ({ ...f, value: e.target.value }))}
            />
          </div>
          <div className="flex justify-end gap-2">
            <button
              onClick={() => { setAddForm({ isOpen: false, name: '', value: '' }); setFormError(null) }}
              className="text-sm px-3 py-1.5 rounded-md border text-muted-foreground hover:bg-muted"
            >
              Cancel
            </button>
            <button
              onClick={handleSave}
              disabled={setMutation.isPending}
              className="text-sm px-3 py-1.5 bg-primary text-primary-foreground rounded-md hover:bg-primary/90 disabled:opacity-50"
            >
              {setMutation.isPending ? 'Saving...' : 'Save'}
            </button>
          </div>
        </div>
      )}

      {isLoading && <p className="text-sm text-muted-foreground">Loading...</p>}

      {error && (
        <div className="rounded-md bg-red-500/10 border border-red-500/20 p-3 text-sm text-red-400">
          {(error as Error).message}
        </div>
      )}

      {deleteError && (
        <div className="rounded-md bg-red-500/10 border border-red-500/20 p-3 text-sm text-red-400">
          Delete failed: {deleteError}
        </div>
      )}

      {!isLoading && !error && creds.length === 0 && (
        <p className="text-sm text-muted-foreground">
          No credentials yet. Click + Add to create one.
        </p>
      )}

      {creds.length > 0 && (
        <div className="rounded-lg border overflow-hidden">
          {creds.map((cred, i) => (
            <div
              key={cred.name}
              className={`flex items-center justify-between px-4 py-3${i < creds.length - 1 ? ' border-b' : ''}`}
            >
              <div>
                <span className="font-mono text-sm">{cred.name}</span>
                <span className="ml-3 text-xs text-muted-foreground">
                  updated {formatRelativeTime(cred.updated_at)}
                </span>
              </div>

              {pendingDelete === cred.name ? (
                <div className="flex items-center gap-2">
                  <span className="text-xs text-muted-foreground">Are you sure?</span>
                  <button
                    onClick={() => deleteMutation.mutate(cred.name)}
                    disabled={deleteMutation.isPending}
                    className="px-2 py-1 bg-red-600 text-white rounded text-xs hover:bg-red-700 disabled:opacity-50"
                  >
                    Confirm
                  </button>
                  <button
                    onClick={() => { setPendingDelete(null); setDeleteError(null) }}
                    className="px-2 py-1 rounded text-xs border text-muted-foreground hover:bg-muted"
                  >
                    Cancel
                  </button>
                </div>
              ) : (
                <button
                  onClick={() => setPendingDelete(cred.name)}
                  className="text-xs px-2 py-1 rounded text-muted-foreground hover:text-red-400 hover:bg-red-500/10"
                >
                  Delete
                </button>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

export function CredentialsPage() {
  const { data: me } = useQuery({
    queryKey: ['me'],
    queryFn: () => api.getMe(),
  })

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-bold">Settings</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Manage credentials available to your workflows.
        </p>
      </div>

      <CredentialSection
        title="Team Credentials"
        queryKey={['credentials', 'team']}
        fetchFn={() => api.listCredentials()}
        setFn={(name, value) => api.setCredential(name, value)}
        deleteFn={(name) => api.deleteCredential(name)}
      />

      {me?.platform_admin && (
        <CredentialSection
          title="System Credentials"
          queryKey={['credentials', 'system']}
          fetchFn={() => api.listSystemCredentials()}
          setFn={(name, value) => api.setSystemCredential(name, value)}
          deleteFn={(name) => api.deleteSystemCredential(name)}
        />
      )}
    </div>
  )
}
