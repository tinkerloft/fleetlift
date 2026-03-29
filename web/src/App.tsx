import type React from 'react'
import { useState, useEffect } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { Layout } from './components/Layout'
import { WorkflowListPage } from './pages/WorkflowList'
import { WorkflowDetailPage } from './pages/WorkflowDetail'
import { RunListPage } from './pages/RunList'
import { RunDetailPage } from './pages/RunDetail'
import { HomePage } from './pages/HomePage'
import { InboxPage } from './pages/Inbox'
import { ReportListPage } from './pages/ReportList'
import { ReportDetailPage } from './pages/ReportDetail'
import { KnowledgePage } from './pages/KnowledgePage'
import { SystemHealthPage } from './pages/SystemHealth'
import { CredentialsPage } from './pages/CredentialsPage'
import { LoginPage } from './pages/Login'
import { AuthCallbackPage } from './pages/AuthCallback'
import { getConfig } from './api/client'

function RequireAuth({ children }: { children: React.ReactNode }) {
  const token = localStorage.getItem('token')
  if (!token) return <Navigate to="/login" replace />
  return <>{children}</>
}

export default function App() {
  const [ready, setReady] = useState(!!localStorage.getItem('token'))

  useEffect(() => {
    if (ready) return
    getConfig().then(cfg => {
      if (cfg.dev_no_auth && !localStorage.getItem('token')) {
        localStorage.setItem('token', 'dev-token')
      }
      setReady(true)
    }).catch(() => setReady(true))
  }, [ready])

  if (!ready) return null

  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/auth/callback" element={<AuthCallbackPage />} />
      <Route path="/*" element={
        <RequireAuth>
          <Layout>
            <Routes>
              <Route path="/" element={<HomePage />} />
              <Route path="/workflows" element={<WorkflowListPage />} />
              <Route path="/workflows/:id" element={<WorkflowDetailPage />} />
              <Route path="/runs" element={<RunListPage />} />
              <Route path="/runs/:id" element={<RunDetailPage />} />
              <Route path="/inbox" element={<InboxPage />} />
              <Route path="/reports" element={<ReportListPage />} />
              <Route path="/reports/:runId" element={<ReportDetailPage />} />
              <Route path="/knowledge" element={<KnowledgePage />} />
              <Route path="system" element={<SystemHealthPage />} />
              <Route path="/settings" element={<CredentialsPage />} />
            </Routes>
          </Layout>
        </RequireAuth>
      } />
    </Routes>
  )
}
