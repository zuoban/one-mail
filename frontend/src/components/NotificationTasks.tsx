import { useEffect, useRef, useState } from 'react'
import { telegramApi, type NotificationList } from '../api'

export default function NotificationTasks() {
  const [result, setResult] = useState<NotificationList | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [version, setVersion] = useState(0)
  const [pendingId, setPendingId] = useState<number | null>(null)
  const lifecycle = useRef(0)
  const mutationPending = useRef(false)

  useEffect(() => {
    const generation = lifecycle.current + 1
    lifecycle.current = generation
    return () => { lifecycle.current = generation + 1 }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    telegramApi.listTasks(controller.signal).then(response => {
      if (!controller.signal.aborted) setResult(response)
    }).catch(() => {
      if (!controller.signal.aborted) setError('投递记录加载失败，请重试')
    }).finally(() => {
      if (!controller.signal.aborted) setLoading(false)
    })
    return () => controller.abort()
  }, [version])

  const reload = () => {
    setError('')
    setLoading(true)
    setVersion(current => current + 1)
  }

  const changeTask = async (id: number, remove: boolean) => {
    if (mutationPending.current) return
    const generation = lifecycle.current
    mutationPending.current = true
    setPendingId(id)
    setError('')
    try {
      if (remove) await telegramApi.deleteTask(id)
      else await telegramApi.retryTask(id)
      if (generation === lifecycle.current) reload()
    } catch {
      if (generation === lifecycle.current) setError(remove ? '清理失败，请重试' : '重新投递失败，请重试')
    } finally {
      if (generation === lifecycle.current) {
        mutationPending.current = false
        setPendingId(null)
      }
    }
  }

  return (
    <section aria-labelledby="notification-tasks-title" className="settings-panel">
      <div className="panel-heading flex flex-wrap items-start justify-between gap-4">
        <div>
          <h2 id="notification-tasks-title" className="text-base font-medium text-[var(--text-primary)]">通知投递记录</h2>
          <p className="mt-1 text-sm text-[var(--text-secondary)]">临时故障会自动重试。修正配置后，可重新投递失败任务或清理记录。</p>
        </div>
        <button type="button" className="btn btn-secondary btn-sm" disabled={loading || pendingId !== null} onClick={reload}>刷新投递记录</button>
      </div>
      {error && <p role="alert" className="mt-4 text-sm text-[var(--error-700)]">{error}</p>}
      {loading && <p aria-live="polite" className="mt-4 text-sm text-[var(--text-secondary)]">正在读取投递记录…</p>}
      {result && (
        <>
          <p className="delivery-summary">待投递 {result.pending_count} 条 · 失败 {result.failed_count} 条</p>
          {result.data.length === 0 ? (
            <p className="mt-3 text-sm text-[var(--text-tertiary)]">没有失败的通知任务</p>
          ) : (
            <ul className="mt-4 divide-y divide-[var(--border-light)]">
              {result.data.map(task => (
                <li key={task.id} className="flex flex-col gap-3 py-4 sm:flex-row sm:items-center sm:justify-between">
                  <div className="min-w-0 text-sm">
                    <p className="break-all text-[var(--text-primary)]">{task.account_email}</p>
                    <p className="mt-1 text-xs leading-5 text-[var(--text-tertiary)]">已尝试 {task.attempts} 次 · <time className="font-mono tabular-nums" dateTime={task.created_at}>{new Date(task.created_at).toLocaleString('zh-CN')}</time></p>
                    <p className="mt-2 break-words text-[var(--error-700)]">{task.last_error.replace(/^permanent: /, '')}</p>
                  </div>
                  <div className="flex flex-shrink-0 gap-2">
                    <button type="button" className="btn btn-secondary btn-sm" disabled={loading || pendingId !== null} onClick={() => void changeTask(task.id, false)}>{pendingId === task.id ? '处理中…' : '重试通知'}</button>
                    <button type="button" className="btn btn-ghost btn-sm" disabled={loading || pendingId !== null} onClick={() => void changeTask(task.id, true)}>清理记录</button>
                  </div>
                </li>
              ))}
            </ul>
          )}
          {result.failed_count > result.data.length && <p className="mt-3 text-xs text-[var(--text-tertiary)]">当前显示最近 {result.data.length} 条失败记录，处理后刷新可查看其余记录。</p>}
        </>
      )}
    </section>
  )
}
