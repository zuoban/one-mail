import { Loader2 } from 'lucide-react'
import useAuth from '../context/useAuth'

export default function AuthStatusFeedback() {
  const { error, retry } = useAuth()
  return (
    <div className="min-h-screen flex items-center justify-center bg-[var(--bg-secondary)] p-6">
      {error ? (
        <div className="max-w-sm text-center space-y-4">
          <p role="alert" className="text-sm text-[var(--text-primary)]">{error}</p>
          <button type="button" className="btn btn-primary" onClick={() => void retry()}>重试</button>
        </div>
      ) : (
        <div role="status" className="flex items-center gap-2 text-sm text-[var(--text-secondary)]">
          <Loader2 aria-hidden="true" className="w-5 h-5 animate-spin" strokeWidth={1.5} />
          <span>正在确认访问状态...</span>
        </div>
      )}
    </div>
  )
}
