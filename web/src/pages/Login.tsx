import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'

export function LoginPage() {
  const navigate = useNavigate()

  useEffect(() => {
    // In dev mode (DEV_NO_AUTH=1) the server exposes /api/auth/dev-login.
    // Auto-obtain a token so the user doesn't have to go through GitHub OAuth.
    fetch('/api/auth/dev-login')
      .then(res => {
        if (!res.ok) return
        return res.json()
      })
      .then((data: { token?: string } | undefined) => {
        if (data?.token) {
          localStorage.setItem('token', data.token)
          navigate('/', { replace: true })
        }
      })
      .catch(() => {
        // Not in dev mode — fall through to GitHub login UI
      })
  }, [navigate])

  return (
    <div className="flex min-h-screen items-center justify-center bg-background">
      <div className="flex flex-col items-center gap-6 rounded-xl border bg-card p-10 shadow-sm">
        <div className="flex h-10 w-10 items-center justify-center rounded-md bg-foreground">
          <span className="text-sm font-bold text-background">FL</span>
        </div>
        <div className="text-center">
          <h1 className="text-xl font-semibold">Fleetlift</h1>
          <p className="mt-1 text-sm text-muted-foreground">Sign in to continue</p>
        </div>
        <a
          href="/auth/github"
          className="flex items-center gap-2 rounded-lg bg-foreground px-5 py-2.5 text-sm font-medium text-background transition-opacity hover:opacity-80"
        >
          <svg viewBox="0 0 24 24" className="h-4 w-4 fill-current" aria-hidden="true">
            <path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0 0 24 12c0-6.63-5.37-12-12-12z" />
          </svg>
          Continue with GitHub
        </a>
      </div>
    </div>
  )
}
