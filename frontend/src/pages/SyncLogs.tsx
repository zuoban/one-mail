import { useState, useEffect } from 'react'
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
    accountIdParam ? parseInt(accountIdParam) : null
  )
  const [loading, setLoading] = useState(false)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(10)
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null)
  const [confirmClear, setConfirmClear] = useState(false)

  useEffect(() => {
    loadAccounts()
    loadData()
  }, [])

  useEffect(() => {
    const interval = setInterval(loadData, 10000)
    return () => clearInterval(interval)
  }, [selectedAccountId, page])

  useEffect(() => {
    loadData()
  }, [selectedAccountId, page])

  const loadAccounts = async () => {
    try {
      const res = await accountApi.list()
      setAccounts(res.data)
    } catch (e) {
      console.error(e)
    }
  }

  const loadData = async () => {
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
  }

  const handleAccountChange = (id: string) => {
    const newId = id ? parseInt(id) : null
    setSelectedAccountId(newId)
    setPage(1)
    if (newId) {
      setSearchParams({ account_id: id })
    } else {
      setSearchParams({})
    }
  }

  const handleClearLogs = async () => {
    if (!selectedAccountId) return
    try {
      await syncApi.clearLogs(selectedAccountId)
      setToast({ message: '日志已清空', type: 'success' })
      loadData()
    } catch {
      setToast({ message: '清空失败', type: 'error' })
    } finally {
      setConfirmClear(false)
      setTimeout(() => setToast(null), 2000)
    }
  }

  const formatTime = (time: string) => {
    if (!time) return '-'
    const date = new Date(time)
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
    return `${(ms / 60000).toFixed(1)}min`
  }

  const getAccountEmail = (accountId: number) => {
    const account = accounts.find(a => a.id === accountId)
    return account?.email || `账户 ${accountId}`
  }

  const totalPages = Math.ceil(total / pageSize)

  return (
    <div className="p-6">
      <ConfirmDialog
        open={confirmClear}
        title="确认清空"
        message="确定要清空该账户的所有同步日志吗？此操作不可撤销。"
        confirmLabel="清空"
        cancelLabel="取消"
        onConfirm={handleClearLogs}
        onCancel={() => setConfirmClear(false)}
      />

      {toast && (
        <div className="fixed top-4 right-4 z-50">
          <div className={`px-4 py-3 rounded-lg text-sm shadow-lg border ${toast.type === 'success' ? 'bg-[var(--success-50)] text-[var(--success-600)] border-[var(--success-100)]' : 'bg-[var(--error-50)] text-[var(--error-600)] border-[var(--error-100)]'}`}>
            {toast.message}
          </div>
        </div>
      )}

      <div className="flex items-center justify-between mb-6">
        <h2 className="text-2xl font-semibold text-[var(--text-primary)]">同步日志</h2>
        <div className="flex items-center gap-3">
          <select
            value={selectedAccountId || ''}
            onChange={e => handleAccountChange(e.target.value)}
            className="px-3 py-2 rounded-lg bg-[var(--bg-secondary)] border border-[var(--border-light)] text-[var(--text-primary)] text-sm focus:outline-none focus:ring-2 focus:ring-[var(--primary-500)]"
          >
            <option value="">全部账户</option>
            {accounts.map(account => (
              <option key={account.id} value={account.id}>
                {account.email}
              </option>
            ))}
          </select>
          <button
            onClick={loadData}
            disabled={loading}
            className="inline-flex items-center gap-2 px-4 py-2 text-sm font-medium rounded-lg text-[var(--text-primary)] hover:text-white bg-[var(--bg-primary)] hover:bg-[var(--primary-500)] border border-[var(--border-light)] transition-all duration-200 whitespace-nowrap disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            <span>刷新</span>
          </button>
          {selectedAccountId && (
            <button
              onClick={() => setConfirmClear(true)}
              className="inline-flex items-center gap-2 px-4 py-2 text-sm font-medium rounded-lg text-[var(--error-600)] hover:text-white bg-[var(--bg-primary)] hover:bg-[var(--error-600)] border border-[var(--error-600)] transition-all duration-200 whitespace-nowrap"
            >
              <Trash2 className="w-4 h-4" />
              <span>清空日志</span>
            </button>
          )}
        </div>
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-12">
          <Loader2 className="w-8 h-8 animate-spin text-[var(--primary-500)]" />
        </div>
      ) : logs.length === 0 ? (
        <div className="empty-state">
          <FileText className="empty-state-icon" />
          <p className="text-lg text-[var(--text-secondary)]">暂无同步日志</p>
          <p className="text-sm mt-2 text-[var(--text-tertiary)]">同步邮件后将在此显示日志记录</p>
        </div>
      ) : (
        <>
          <div className="bg-[var(--bg-primary)] rounded-xl border border-[var(--border-light)] overflow-hidden">
            <table className="w-full">
              <thead>
                <tr className="border-b border-[var(--border-light)] bg-[var(--bg-secondary)]">
                  <th className="px-4 py-3 text-left text-xs font-semibold text-[var(--text-tertiary)] uppercase tracking-wider">状态</th>
                  <th className="px-4 py-3 text-left text-xs font-semibold text-[var(--text-tertiary)] uppercase tracking-wider">账户</th>
                  <th className="px-4 py-3 text-left text-xs font-semibold text-[var(--text-tertiary)] uppercase tracking-wider">开始时间</th>
                  <th className="px-4 py-3 text-left text-xs font-semibold text-[var(--text-tertiary)] uppercase tracking-wider">耗时</th>
                  <th className="px-4 py-3 text-left text-xs font-semibold text-[var(--text-tertiary)] uppercase tracking-wider">新增邮件</th>
                  <th className="px-4 py-3 text-left text-xs font-semibold text-[var(--text-tertiary)] uppercase tracking-wider">错误信息</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--border-light)]">
                {logs.map(log => (
                  <tr key={log.id} className="hover:bg-[var(--bg-secondary)] transition-colors">
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2">
                        {log.status === 'success' ? (
                          <CheckCircle className="w-4 h-4 text-[var(--success-600)]" />
                        ) : log.status === 'failed' ? (
                          <XCircle className="w-4 h-4 text-[var(--error-600)]" />
                        ) : (
                          <Loader2 className="w-4 h-4 text-[var(--primary-600)] animate-spin" />
                        )}
                        <span className={`text-sm font-medium ${
                          log.status === 'success' ? 'text-[var(--success-600)]' :
                          log.status === 'failed' ? 'text-[var(--error-600)]' :
                          'text-[var(--primary-600)]'
                        }`}>
                          {log.status === 'success' ? '成功' : log.status === 'failed' ? '失败' : '进行中'}
                        </span>
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <span className="text-sm text-[var(--text-primary)]">{getAccountEmail(log.account_id)}</span>
                    </td>
                    <td className="px-4 py-3">
                      <span className="text-sm text-[var(--text-secondary)]">{formatTime(log.start_time)}</span>
                    </td>
                    <td className="px-4 py-3">
                      <span className="text-sm text-[var(--text-secondary)]">{formatDuration(log.duration_ms)}</span>
                    </td>
                    <td className="px-4 py-3">
                      <span className="text-sm text-[var(--text-secondary)]">{log.new_count}</span>
                    </td>
                    <td className="px-4 py-3">
                      {log.error ? (
                        <span className="text-sm text-[var(--error-600)] truncate max-w-xs block" title={log.error}>
                          {log.error}
                        </span>
                      ) : (
                        <span className="text-sm text-[var(--text-tertiary)]">-</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {totalPages > 1 && (
            <div className="flex items-center justify-between mt-6 pt-4 border-t border-[var(--border-light)]">
              <span className="text-sm text-[var(--text-tertiary)]">
                共 {total} 条记录
              </span>
              <div className="flex items-center gap-1">
                <button
                  onClick={() => setPage(1)}
                  disabled={page === 1}
                  className="p-1.5 rounded-lg text-[var(--text-tertiary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-tertiary)] disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
                  title="第一页"
                >
                  <ChevronsLeft className="w-4 h-4" />
                </button>
                <button
                  onClick={() => setPage(p => Math.max(1, p - 1))}
                  disabled={page === 1}
                  className="p-1.5 rounded-lg text-[var(--text-tertiary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-tertiary)] disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
                  title="上一页"
                >
                  <ChevronLeft className="w-4 h-4" />
                </button>
                
                {Array.from({ length: Math.min(5, totalPages) }, (_, i) => {
                  let pageNum: number
                  if (totalPages <= 5) {
                    pageNum = i + 1
                  } else if (page <= 3) {
                    pageNum = i + 1
                  } else if (page >= totalPages - 2) {
                    pageNum = totalPages - 4 + i
                  } else {
                    pageNum = page - 2 + i
                  }
                  return (
                    <button
                      key={pageNum}
                      onClick={() => setPage(pageNum)}
                      className={`w-8 h-8 rounded-lg text-sm font-medium transition-colors ${
                        pageNum === page
                          ? 'bg-[var(--primary-500)] text-white'
                          : 'text-[var(--text-secondary)] hover:bg-[var(--bg-tertiary)]'
                      }`}
                    >
                      {pageNum}
                    </button>
                  )
                })}
                
                <button
                  onClick={() => setPage(p => Math.min(totalPages, p + 1))}
                  disabled={page === totalPages}
                  className="p-1.5 rounded-lg text-[var(--text-tertiary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-tertiary)] disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
                  title="下一页"
                >
                  <ChevronRight className="w-4 h-4" />
                </button>
                <button
                  onClick={() => setPage(totalPages)}
                  disabled={page === totalPages}
                  className="p-1.5 rounded-lg text-[var(--text-tertiary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-tertiary)] disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
                  title="最后一页"
                >
                  <ChevronsRight className="w-4 h-4" />
                </button>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}
