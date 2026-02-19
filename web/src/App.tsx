import { Routes, Route } from 'react-router-dom'
import { Layout } from './components/Layout'
import { InboxPage } from './pages/Inbox'
import { TaskListPage } from './pages/TaskList'
import { TaskDetailPage } from './pages/TaskDetail'

export default function App() {
  return (
    <Layout>
      <Routes>
        <Route path="/" element={<InboxPage />} />
        <Route path="/tasks" element={<TaskListPage />} />
        <Route path="/tasks/:id" element={<TaskDetailPage />} />
      </Routes>
    </Layout>
  )
}
