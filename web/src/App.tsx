import { Routes, Route, Navigate } from 'react-router-dom'
import { Layout } from './components/Layout'
import { WorkflowListPage } from './pages/WorkflowList'
import { WorkflowDetailPage } from './pages/WorkflowDetail'
import { RunListPage } from './pages/RunList'
import { RunDetailPage } from './pages/RunDetail'
import { InboxPage } from './pages/Inbox'
import { ReportListPage } from './pages/ReportList'
import { ReportDetailPage } from './pages/ReportDetail'
import { KnowledgePage } from './pages/KnowledgePage'

export default function App() {
  return (
    <Layout>
      <Routes>
        <Route path="/" element={<Navigate to="/runs" replace />} />
        <Route path="/workflows" element={<WorkflowListPage />} />
        <Route path="/workflows/:id" element={<WorkflowDetailPage />} />
        <Route path="/runs" element={<RunListPage />} />
        <Route path="/runs/:id" element={<RunDetailPage />} />
        <Route path="/inbox" element={<InboxPage />} />
        <Route path="/reports" element={<ReportListPage />} />
        <Route path="/reports/:runId" element={<ReportDetailPage />} />
        <Route path="/knowledge" element={<KnowledgePage />} />
      </Routes>
    </Layout>
  )
}
