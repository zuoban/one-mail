import { useState, useEffect, useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import { syncApi, accountApi } from '../api'
import type { SyncLog, EmailAccount } from '../api'
import { CheckCircle, XCircle, Loader2, Trash2, FileText, ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight, RefreshCw } from 'lucide-react'
import ConfirmDialog from '../components/ConfirmDialog'

export default function SyncLogs() {
  const [searchParams, setSearchParams] = useSearchParams()
  const accountIdParam = searchParams.get('account_id')
  const [logs, setLogs] = useState<SyncLog[]>([])
  const [accounts, setAccounts] = useState<EmailAccount[]>([])
  const [selectedAccountId, setSelectedAccountId] = useState<number | null>(
    accountIdParam ? parseInt(accountIdParam, 10) : null,
  )
  const [loading, setLoading] = useState(false)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(10)
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null)
  const [confirmClear, setConfirmClear] = useState(false)
  const [statusFilter, setStatusFilter] = useState<'all' | 'success' | 'failed'>('all')
  const [expandedLogId, setExpandedLogId] = useState<number | null>(null)

  useEffect(() => {
    accountApi.list().then(res => setAccounts(res.data)).catch(console.error)
  }, [])

  const loadData = useCallback(async () => {
    setLoading(true)
    try {
      const res = selectedAccountId
        ? await syncApi.getLogsByAccount(selectedAccountId, { page, page_size: pageSize })
        : await syncApi.getLogs({ page, page_size: pageSize })
      setLogs(res.data || [])
      setTotal(res.total || 0)
    } catch (e) {
      console.error(e)
    } finally {
      setLoading(false)
    }
  }, [selectedAccountId, page, pageSize])

  useEffect(() => {
    const params = new URLSearchParams()
    if (selectedAccountId) params.set('account_id', String(selectedAccountId))
    params.set('page', String(page))
    setSearchParams(params, { replace: true })
    loadData()
  }, [selectedAccountId, page, setSearchParams, loadData])

  const handleAccountChange = (id: string) => {
    const newId = id ? parseInt(id, 10) : null
    setSelectedAccountId(newId)
    setPage(1)
  }

  const handleClearLogs = async () => {
    if (!selectedAccountId) return
    setConfirmClear(false)
    try {
      await syncApi.clearLogs(selectedAccountId)
      setToast({ message: '日志已清空', type: 'success' })
      window.setTimeout(() => setToast(null), 2000)
      loadData()
    } catch {
      setToast({ message: '清空日志失败', type: 'error' })
      window.setTimeout(() => setToast(null), 2000)
    }
  }

  const formatTime = (time: string) => {
    if (!time) return '-'
    const date = new Date(time)
    if (Number.isNaN(date.getTime())) return '-'
    return date.toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
  }

  const formatDuration = (ms: number) => {
    if (ms < 1000) return `${ms}ms`
    if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
    return `${(ms / 60000).toFixed(1)}m`
  }

  const totalPages = Math.ceil(total / pageSize)
  const visibleLogs = statusFilter === 'all' ? logs : logs.filter(log => log.status === statusFilter)
  return (
    <div className="max-w-5xl mx-auto p-4 md:p-8">
      <ConfirmDialog
        open={confirmClear}
        title="清空同步日志"
        message="确定要清空该账户的所有同步日志吗？此操作不可撤销。"
        confirmLabel="清空"
        cancelLabel="取消"
        onConfirm={handleClearLogs}
        onCancel={() => setConfirmClear(false)}
      />

      {toast && (
        <div className="fixed left-4 right-4 top-4 z-50 md:left-auto md:right-4">
          <div
            className={`flex items-center gap-2.5 px-4 py-2.5 rounded-[8px] text-sm border shadow-[var(--shadow-md)] ${
              toast.type === 'success'
                ? 'bg-[var(--bg-primary)] text-[var(--text-primary)] border-[var(--border-default)]'
                : 'bg-[var(--error-50)] text-[var(--error-700)] border-[var(--error-100)]'
            }`}
          >
            <span className={`status-dot ${toast.type === 'success' ? 'status-dot-success' : 'status-dot-error'}`} />
            <span>{toast.message}</span>
          </div>
        </div>
      )}

      <div className="mb-6 flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 className="text-2xl font-medium tracking-[-0.015em] text-[var(--text-primary)]">同步日志</h1>
          <p className="mt-1 text-sm text-[var(--text-secondary)]">
            按账户查看每次 IMAP 拉取耗时、新增邮件数与错误详情
          </p>
        </div>

        <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
          <div className="segmented self-start sm:self-auto">
            {(['all', 'success', 'failed'] as const).map((status) => (
              <button
                key={status}
                type="button"
                onClick={() => setStatusFilter(status)}
                className={`segmented-item h-7 text-xs ${statusFilter === status ? 'segmented-item-active' : ''}`}
              >
                {status === 'all' ? '全部状态' : status === 'success' ? '成功' : '失败'}
              </button>
            ))}
          </div>
          <select
            value={selectedAccountId || ''}
            onChange={e => handleAccountChange(e.target.value)}
            className="w-full sm:w-52"
          >
            <option value="">全部账户</option>
            {accounts.map(account => (
              <option key={account.id} value={account.id}>
                {account.email}
              </option>
            ))}
          </select>
          <button
            type="button"
            onClick={loadData}
            disabled={loading}
            className="btn btn-secondary w-full sm:w-auto"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} strokeWidth={1.5} />
            <span>刷新</span>
          </button>
          {selectedAccountId && (
            <button
              type="button"
              onClick={() => setConfirmClear(true)}
              className="btn btn-danger-ghost w-full sm:w-auto"
            >
              <Trash2 className="w-4 h-4" strokeWidth={1.5} />
              <span>清空日志</span>
            </button>
          )}
        </div>
      </div>

      {loading ? (
        <div className="rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] shadow-[var(--shadow-sm)] flex items-center justify-center py-16 gap-2 text-sm text-[var(--text-tertiary)]">
          <Loader2 className="w-4 h-4 animate-spin" strokeWidth={1.5} />
          <span>加载中</span>
        </div>
      ) : visibleLogs.length === 0 ? (
        <div className="rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] shadow-[var(--shadow-sm)] empty-state">
          <FileText className="empty-state-icon" />
          <p className="text-base font-medium text-[var(--text-primary)]">暂无匹配日志</p>
          <p className="text-sm mt-1 text-[var(--text-tertiary)]">
            {statusFilter === 'all' ? '同步邮件后将在此记录每次任务结果' : '当前状态筛选下没有日志记录'}
          </p>
        </div>
      ) : (
        <>
          <div className="rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] shadow-[var(--shadow-sm)] overflow-hidden">
            <div className="overflow-x-auto thin-scrollbar">
              <table className="w-full min-w-[720px] border-collapse">
                <thead>
                  <tr className="border-b border-[var(--border-light)] bg-[var(--bg-secondary)]">
                    <th className="px-4 py-3 text-left text-xs font-medium text-[var(--text-tertiary)] whitespace-nowrap">状态</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-[var(--text-tertiary)] whitespace-nowrap">账户</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-[var(--text-tertiary)] whitespace-nowrap">开始时间</th>
                    <th className="px-4 py-3 text-right text-xs font-medium text-[var(--text-tertiary)] whitespace-nowrap">耗时</th>
                    <th className="px-4 py-3 text-right text-xs font-medium text-[var(--text-tertiary)] whitespace-nowrap">新增邮件</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-[var(--text-tertiary)] whitespace-nowrap">错误信息</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[var(--border-light)]">
                  {visibleLogs.map(log => {
                    const accountEmail = accounts.find(a => a.id === log.account_id)?.email || `账户 #${log.account_id}`
                    const isExpanded = expandedLogId === log.id
                    return (
                      <tr
                        key={log.id}
                        onClick={() => log.error && setExpandedLogId(isExpanded ? null : log.id)}
                        className={`transition-colors hover:bg-[var(--bg-nav-hover)] ${log.error ? 'cursor-pointer' : ''}`}
                      >
                        <td className="px-4 py-3 whitespace-nowrap align-top">
                          <div className="inline-flex items-center gap-2">
                            {log.status === 'success' ? (
                              <CheckCircle className="w-4 h-4 text-[var(--success-600)]" strokeWidth={1.5} />
                            ) : log.status === 'failed' ? (
                              <XCircle className="w-4 h-4 text-[var(--error-600)]" strokeWidth={1.5} />
                            ) : (
                              <Loader2 className="w-4 h-4 text-[var(--primary-500)] animate-spin" strokeWidth={1.5} />
                            )}
                            <span
                              className={`text-sm ${
                                log.status === 'success'
                                  ? 'text-[var(--text-primary)]'
                                  : log.status === 'failed'
                                  ? 'text-[var(--error-600)]'
                                  : 'text-[var(--primary-600)]'
                              }`}
                            >
                              {log.status === 'success' ? '成功' : log.status === 'failed' ? '失败' : '进行中'}
                            </span>
                          </div>
                        </td>
                        <td className="px-4 py-3 text-sm font-mono text-[var(--text-primary)] whitespace-nowrap align-top">
                          {accountEmail}
                        </td>
                        <td className="px-4 py-3 text-sm font-mono tabular-nums text-[var(--text-secondary)] whitespace-nowrap align-top">
                          {formatTime(log.start_time)}
                        </td>
                        <td className="px-4 py-3 text-right text-sm font-mono tabular-nums text-[var(--text-secondary)] whitespace-nowrap align-top">
                          {formatDuration(log.duration_ms)}
                        </td>
                        <td className="px-4 py-3 text-right text-sm font-mono tabular-nums text-[var(--text-primary)] whitespace-nowrap align-top">
                          {log.new_count}
                        </td>
                        <td className="px-4 py-3 align-top">
                          {log.error ? (
                            <div
                              className={`text-xs font-mono text-[var(--error-600)] ${
                                isExpanded ? 'whitespace-pre-wrap break-words max-w-md' : 'truncate max-w-xs'
                              }`}
                              title={isExpanded ? '点击收起' : '点击展开完整错误'}
                            >
                              {log.error}
                            </div>
                          ) : (
                            <span className="text-xs text-[var(--text-tertiary)]">—</span>
                          )}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          </div>

          {totalPages > 1 && (
            <div className="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <span className="font-mono tabular-nums text-xs text-[var(--text-tertiary)]">
                共 {total} 条记录 · 第 {page}/{totalPages} 页
              </span>
              <div className="flex items-center gap-1 flex-wrap">
                <button
                  type="button"
                  onClick={() => setPage(1)}
                  disabled={page === 1}
                  className="btn btn-ghost w-8 h-8 p-0"
                  title="第一页"
                >
                  <ChevronsLeft className="w-4 h-4" strokeWidth={1.5} />
                </button>
                <button
                  type="button"
                  onClick={() => setPage(p => Math.max(1, p - 1))}
                  disabled={page === 1}
                  className="btn btn-ghost w-8 h-8 p-0"
                  title="上一页"
                >
                  <ChevronLeft className="w-4 h-4" strokeWidth={1.5} />
                </button>

                {Array.from({ length: Math.min(5, totalPages) }, (_, i) => {
                  let pageNum: number
                  if (totalPages <= 5) pageNum = i + 1
                  else if (page <= 3) pageNum = i + 1
                  else if (page >= totalPages - 2) pageNum = totalPages - 4 + i
                  else pageNum = page - 2 + i

                  return (
                    <button
                      key={pageNum}
                      type="button"
                      onClick={() => setPage(pageNum)}
                      className={`w-8 h-8 rounded-[6px] font-mono tabular-nums text-xs transition-colors cursor-pointer ${
                        pageNum === page
                          ? 'bg-[var(--bg-tertiary)] text-[var(--text-primary)] font-medium border border-[var(--border-default)]'
                          : 'text-[var(--text-secondary)] hover:bg-[var(--bg-tertiary)]/50'
                      }`}
                    >
                      {pageNum}
                    </button>
                  )
                })}

                <button
                  type="button"
                  onClick={() => setPage(p => Math.min(totalPages, p + 1))}
                  disabled={page === totalPages}
                  className="btn btn-ghost w-8 h-8 p-0"
                  title="下一页"
                >
                  <ChevronRight className="w-4 h-4" strokeWidth={1.5} />
                </button>
                <button
                  type="button"
                  onClick={() => setPage(totalPages)}
                  disabled={page === totalPages}
                  className="btn btn-ghost w-8 h-8 p-0"
                  title="最后一页"
                >
                  <ChevronsRight className="w-4 h-4" strokeWidth={1.5} />
                </button>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}
