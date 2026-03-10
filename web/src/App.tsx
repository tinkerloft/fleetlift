import { Routes, Route } from 'react-router-dom'
import { Layout } from './components/Layout'
import { DashboardPage } from './pages/Dashboard'
import { InboxPage } from './pages/Inbox'
import { TaskListPage } from './pages/TaskList'
import { TaskDetailPage } from './pages/TaskDetail'
import { TaskCreatePage } from './pages/TaskCreate'
import { TemplatesPage } from './pages/TemplatesPage'
import { KnowledgePage } from './pages/KnowledgePage'
import { KnowledgeReviewPage } from './pages/KnowledgeReview'

export default function App() {
  return (
    <Layout>
      <Routes>
        <Route path="/" element={<DashboardPage />} />
        <Route path="/inbox" element={<InboxPage />} />
        <Route path="/tasks" element={<TaskListPage />} />
        <Route path="/tasks/:id" element={<TaskDetailPage />} />
        <Route path="/create" element={<TaskCreatePage />} />
        <Route path="/templates" element={<TemplatesPage />} />
        <Route path="/knowledge" element={<KnowledgePage />} />
        <Route path="/knowledge/review" element={<KnowledgeReviewPage />} />
      </Routes>
    </Layout>
  )
}
