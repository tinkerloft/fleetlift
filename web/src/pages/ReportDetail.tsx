import { useParams } from 'react-router-dom'
import { ReportViewer } from '@/components/ReportViewer'

export function ReportDetailPage() {
  const { runId } = useParams<{ runId: string }>()

  if (!runId) {
    return <p className="text-muted-foreground text-sm">No run ID provided</p>
  }

  return <ReportViewer runId={runId} />
}
