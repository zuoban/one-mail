import { useState, useEffect, useCallback, useRef, useMemo } from 'react'
import { emailApi, accountApi, syncApi, telegramApi } from '../api'
import axios from 'axios'
import ConfirmDialog from '../components/ConfirmDialog'
import Tooltip from '../components/Tooltip'
import type { Email, EmailAccount, SyncStatus } from '../api'
import { Search, Mail, Paperclip, Trash2, Eye, EyeOff, Loader2, Send } from 'lucide-react'

const providers = [
  { value: 'gmail', label: 'Gmail' },
  { value: 'qq', label: 'QQ邮箱' },
  { value: 'outlook', label: 'Outlook' },
  { value: 'qq-work', label: 'QQ企业邮' },
  { value: '163', label: '163邮箱' },
  { value: 'custom', label: '自定义' },
]


export default function Dashboard() {
  const [emails, setEmails] = useState<Email[]>([])
  const [accounts, setAccounts] = useState<EmailAccount[]>([])
  const [selectedAccountId, setSelectedAccountId] = useState<number | null>(null)
  const [selectedEmail, setSelectedEmail] = useState<Email | null>(null)
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState<'all' | 'unread'>('all')
  const [syncStatuses, setSyncStatuses] = useState<Record<string, SyncStatus>>({})
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [hasMore, setHasMore] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [toast, setToast] = useState<{
    message: string
    type: 'success' | 'error'
    actionLabel?: string
    onAction?: () => void
  } | null>(null)
  const toastTimer = useRef<number | null>(null)
  const deleteTimers = useRef<Record<number, number>>({})
  const [pendingDeletes, setPendingDeletes] = useState<Record<number, Email>>({})
  const [contextMenu, setContextMenu] = useState<{
    email: Email
    x: number
    y: number
  } | null>(null)
  const [contextMenuIndex, setContextMenuIndex] = useState(0)
  const emailItemRefs = useRef<Record<number, HTMLDivElement | null>>({})
  const detailScrollRef = useRef<HTMLDivElement | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<{ email: Email } | null>(null)
  const [sendingTelegram, setSendingTelegram] = useState(false)

  const formatSyncTime = (value?: string) => {
    if (!value) return '未同步'
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) return '未同步'
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    const diffMinutes = Math.floor(diffMs / (1000 * 60))
    if (diffMinutes < 1) return '刚刚同步'
    if (diffMinutes < 60) return `${diffMinutes} 分钟前`
    const diffHours = Math.floor(diffMinutes / 60)
    if (diffHours < 24) return `${diffHours} 小时前`
    const diffDays = Math.floor(diffHours / 24)
    if (diffDays < 7) return `${diffDays} 天前`
    return date.toLocaleDateString('zh-CN')
  }

  const formatRelativeTime = (date: Date | null) => {
    if (!date) return '未知日期'
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    if (diffMs < 0) return date.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
    
    const diffMinutes = Math.floor(diffMs / (1000 * 60))
    if (diffMinutes < 1) return '刚刚'
    if (diffMinutes < 60) return `${diffMinutes}分钟前`
    
    const diffHours = Math.floor(diffMinutes / 60)
    if (diffHours < 24) return `${diffHours}小时前`
    
    const diffDays = Math.floor(diffHours / 24)
    if (diffDays === 1) return '昨天'
    if (diffDays < 7) return `${diffDays}天前`
    
    const nowYear = now.getFullYear()
    const dateYear = date.getFullYear()
    if (dateYear === nowYear) {
      return date.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
    }
    return date.toLocaleDateString('zh-CN', { year: 'numeric', month: 'short', day: 'numeric' })
  }

  const getAccountColor = (accountId: number) => {
    const account = accounts.find(a => a.id === accountId)
    return account?.color || '#6366f1'
  }


  const loadAccounts = useCallback(async () => {
    try {
      const res = await accountApi.list()
      setAccounts(res.data)
    } catch (e) {
      console.error(e)
    }
  }, [])

  const loadSyncStatuses = useCallback(async () => {
    try {
      const res = await syncApi.getAllStatuses()
      setSyncStatuses(res.data)
    } catch (e) {
      console.error(e)
    }
  }, [])

  const loadingMoreRef = useRef(false)
  const hasMoreRef = useRef(true)
  
  const loadEmails = useCallback(async (pageNum: number = 1, append: boolean = false) => {
    if (pageNum > 1 && loadingMoreRef.current) return
    if (pageNum > 1 && !hasMoreRef.current) return
    
    loadingMoreRef.current = true
    setLoadingMore(true)
    
    try {
      const res = await emailApi.list({ 
        page: pageNum, 
        page_size: 50, 
        search,
        account_id: selectedAccountId || undefined,
      })
      
      if (append) {
        setEmails(prev => [...prev, ...res.data])
      } else {
        setEmails(res.data)
      }
      
      setTotal(res.total)
      const hasMoreVal = pageNum * 50 < res.total
      hasMoreRef.current = hasMoreVal
      setHasMore(hasMoreVal)
      setPage(pageNum)
    } catch (e) {
      console.error(e)
    } finally {
      loadingMoreRef.current = false
      setLoadingMore(false)
    }
  }, [search, selectedAccountId])

  const handleScroll = useCallback((e: React.UIEvent<HTMLDivElement>) => {
    const target = e.target as HTMLDivElement
    const { scrollTop, scrollHeight, clientHeight } = target
    
    if (scrollHeight - scrollTop - clientHeight < 200 && !loadingMore && hasMore) {
      loadEmails(page + 1, true)
    }
  }, [loadEmails, page, hasMore, loadingMore])

  useEffect(() => {
    loadAccounts()
    loadEmails(1, false)
    loadSyncStatuses()
  }, [loadAccounts, loadEmails, loadSyncStatuses])

  useEffect(() => {
    setPage(1)
    setTotal(0)
    setHasMore(true)
    loadEmails(1, false)
  }, [search, selectedAccountId, loadEmails])

  const showToast = useCallback((
    message: string,
    type: 'success' | 'error',
    actionLabel?: string,
    onAction?: () => void,
  ) => {
    setToast({ message, type, actionLabel, onAction })
    if (toastTimer.current) {
      window.clearTimeout(toastTimer.current)
    }
    toastTimer.current = window.setTimeout(() => {
      setToast(null)
    }, 1600)
  }, [])

  const parseEmailDate = useCallback((value?: string | null) => {
    if (!value) return null
    const direct = new Date(value)
    if (!Number.isNaN(direct.getTime())) return direct

    const goTimeMatch = value.match(/^(\d{4}-\d{2}-\d{2})[ T](\d{2}:\d{2}:\d{2})(?:\.\d+)?\s([+-]\d{4})/)
    if (goTimeMatch) {
      const [, datePart, timePart, offset] = goTimeMatch
      const offsetWithColon = offset.replace(/([+-]\d{2})(\d{2})/, '$1:$2')
      const normalized = `${datePart}T${timePart}${offsetWithColon}`
      const parsed = new Date(normalized)
      if (!Number.isNaN(parsed.getTime())) return parsed
    }

    const normalizedLocal = value.replace(' ', 'T')
    const localParsed = new Date(normalizedLocal)
    if (!Number.isNaN(localParsed.getTime())) return localParsed

    return null
  }, [])

  const resolveEmailDate = useCallback((email: Email) => {
    const primary = email.date || email.created_at
    const fallback = email.created_at || email.date
    const primaryDate = parseEmailDate(primary)
    if (primaryDate) return primaryDate
    const fallbackDate = parseEmailDate(fallback)
    if (fallbackDate) return fallbackDate
    return null
  }, [parseEmailDate])

  const getEmailTimestamp = useCallback((email: Email) => {
    const date = resolveEmailDate(email)
    return date ? date.getTime() : 0
  }, [resolveEmailDate])

  const handleRead = useCallback(async (email: Email, shouldScroll = false, scrollDetail = false) => {
    if (!email.is_read) {
      await emailApi.markAsRead(email.id)
      setEmails(prev => prev.map(e => e.id === email.id ? { ...e, is_read: true } : e))
    }

    // 获取邮件详情（包含正文内容）
    try {
      const detail = await emailApi.get(email.id)
      setSelectedEmail(detail.data)
    } catch (e) {
      console.error('Failed to fetch email detail:', e)
      setSelectedEmail(email)
    }

    if (shouldScroll) {
      const node = emailItemRefs.current[email.id]
      if (node) {
        node.scrollIntoView({ block: 'nearest' })
      }
    }
    if (scrollDetail && detailScrollRef.current) {
      detailScrollRef.current.scrollTo({ top: 0, behavior: 'smooth' })
    }
  }, [])

  const handleMarkAsUnread = useCallback(async (email: Email) => {
    if (!email.is_read) return
    try {
      await emailApi.markAsUnread(email.id)
      setEmails(prev => prev.map(e => e.id === email.id ? { ...e, is_read: false } : e))
      if (selectedEmail?.id === email.id) {
        setSelectedEmail({ ...email, is_read: false })
      }
      showToast('已标记为未读', 'success')
    } catch (e) {
      console.error(e)
      showToast('标记未读失败', 'error')
    }
  }, [selectedEmail, showToast])

  const handleDeleteEmail = useCallback(async (email: Email) => {
    if (deleteTimers.current[email.id]) return
    setPendingDeletes(prev => ({ ...prev, [email.id]: email }))
    setEmails(prev => prev.filter(e => e.id !== email.id))
    if (selectedEmail?.id === email.id) {
      setSelectedEmail(null)
    }

    const timerId = window.setTimeout(async () => {
      try {
        await emailApi.delete(email.id)
        setPendingDeletes(prev => {
          const next = { ...prev }
          delete next[email.id]
          return next
        })
      } catch (e) {
        console.error(e)
        setEmails(prev => [...prev, email])
        setPendingDeletes(prev => {
          const next = { ...prev }
          delete next[email.id]
          return next
        })
        showToast('删除失败', 'error')
      } finally {
        delete deleteTimers.current[email.id]
      }
    }, 2200)

    deleteTimers.current[email.id] = timerId
    showToast('已删除邮件', 'success', '撤销', () => {
      const pending = pendingDeletes[email.id] || email
      if (deleteTimers.current[email.id]) {
        window.clearTimeout(deleteTimers.current[email.id])
        delete deleteTimers.current[email.id]
      }
      setPendingDeletes(prev => {
        const next = { ...prev }
        delete next[email.id]
        return next
      })
      setEmails(prev => [...prev, pending])
      showToast('已撤销删除', 'success')
    })
  }, [selectedEmail, showToast])

  const requestDeleteEmail = useCallback((email: Email) => {
    setConfirmDelete({ email })
  }, [])

  const confirmDeleteEmail = useCallback(() => {
    if (!confirmDelete) return
    handleDeleteEmail(confirmDelete.email)
    setConfirmDelete(null)
  }, [confirmDelete, handleDeleteEmail])

  const cancelDeleteEmail = useCallback(() => {
    setConfirmDelete(null)
  }, [])

  const handleSendToTelegram = useCallback(async (email: Email) => {
    setSendingTelegram(true)
    try {
      await telegramApi.sendEmail(email.id)
      showToast('已发送到 Telegram', 'success')
    } catch (e) {
      const message = axios.isAxiosError(e) ? e.response?.data?.error : undefined
      showToast(message || '发送失败', 'error')
    } finally {
      setSendingTelegram(false)
    }
  }, [showToast])

  const closeContextMenu = useCallback(() => {
    setContextMenu(null)
    setContextMenuIndex(0)
  }, [])

  const filteredEmails = useMemo(() => {
    if (filter === 'unread') return emails.filter(email => !email.is_read)
    return emails
  }, [emails, filter])


  const sortedFilteredEmails = useMemo(() =>
    [...filteredEmails].sort((a, b) =>
      getEmailTimestamp(b) - getEmailTimestamp(a),
    ),
    [filteredEmails, getEmailTimestamp]
  )

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (contextMenu) return
      const target = event.target as HTMLElement | null
      if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) {
        return
      }

      if (event.key === 'j') {
        event.preventDefault()
        const index = sortedFilteredEmails.findIndex(email => email.id === selectedEmail?.id)
        const nextEmail = sortedFilteredEmails[index + 1] || sortedFilteredEmails[0]
        if (nextEmail) {
          handleRead(nextEmail, true)
        }
      }

      if (event.key === 'k') {
        event.preventDefault()
        const index = sortedFilteredEmails.findIndex(email => email.id === selectedEmail?.id)
        const prevEmail = index <= 0 ? sortedFilteredEmails[sortedFilteredEmails.length - 1] : sortedFilteredEmails[index - 1]
        if (prevEmail) {
          handleRead(prevEmail, true)
        }
      }

      if (event.key === 'o' || event.key === 'O') {
        event.preventDefault()
        const target = selectedEmail || sortedFilteredEmails[0]
        if (target) {
          handleRead(target, true, true)
        }
      }

      if (event.key === 'r' || event.key === 'R') {
        event.preventDefault()
        loadEmails().then(() => showToast('收件箱已刷新', 'success'))
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [loadEmails, handleRead, selectedEmail, sortedFilteredEmails, contextMenu])

  const contextMenuItems = useMemo(() => {
    if (!contextMenu) return []
    const items: Array<{ label: string; action: () => void | Promise<void>; danger?: boolean }> = [
      { label: '打开', action: () => handleRead(contextMenu.email) },
    ]
    if (contextMenu.email.is_read) {
      items.push({ label: '标记为未读', action: () => handleMarkAsUnread(contextMenu.email) })
    } else {
      items.push({ label: '标记为已读', action: () => handleRead(contextMenu.email) })
    }
    items.push({ label: '删除', action: () => requestDeleteEmail(contextMenu.email), danger: true })
    return items
  }, [contextMenu, requestDeleteEmail, handleMarkAsUnread, handleRead])

  const sanitizedHtml = useMemo(() => {
    if (!selectedEmail?.body_html) return ''
    try {
      const parser = new DOMParser()
      const doc = parser.parseFromString(selectedEmail.body_html, 'text/html')
      doc.querySelectorAll('style, link[rel="stylesheet"], script').forEach(node => node.remove())
      doc.querySelectorAll('img').forEach(img => {
        img.setAttribute('loading', 'lazy')
      })
      return doc.body?.innerHTML || ''
    } catch (e) {
      console.error(e)
      return ''
    }
  }, [selectedEmail?.body_html])

  useEffect(() => {
    if (!contextMenu) return
    const handleMenuKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'ArrowDown') {
        event.preventDefault()
        setContextMenuIndex(prev => (prev + 1) % contextMenuItems.length)
      }
      if (event.key === 'ArrowUp') {
        event.preventDefault()
        setContextMenuIndex(prev => (prev - 1 + contextMenuItems.length) % contextMenuItems.length)
      }
      if (event.key === 'Enter') {
        event.preventDefault()
        const item = contextMenuItems[contextMenuIndex]
        if (item) {
          item.action()
          closeContextMenu()
        }
      }
      if (event.key === 'Escape') {
        event.preventDefault()
        closeContextMenu()
      }
    }
    window.addEventListener('keydown', handleMenuKeyDown)
    return () => window.removeEventListener('keydown', handleMenuKeyDown)
  }, [contextMenu, contextMenuIndex, contextMenuItems, closeContextMenu])

  useEffect(() => {
    return () => {
      Object.values(deleteTimers.current).forEach(timer => window.clearTimeout(timer))
      deleteTimers.current = {}
    }
  }, [])

  useEffect(() => {
    if (!contextMenu) return
    const handleClick = () => setContextMenu(null)
    const handleEsc = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setContextMenu(null)
      }
    }
    window.addEventListener('click', handleClick)
    window.addEventListener('contextmenu', handleClick)
    window.addEventListener('keydown', handleEsc)
    return () => {
      window.removeEventListener('click', handleClick)
      window.removeEventListener('contextmenu', handleClick)
      window.removeEventListener('keydown', handleEsc)
    }
  }, [contextMenu])

  return (
    <div className="h-full flex bg-[var(--bg-secondary)] overflow-hidden">
      {/* Toast Notification */}
      {toast && (
        <div className="fixed top-4 right-4 z-50">
          <div className={`flex items-center gap-3 px-4 py-3 rounded-lg text-sm shadow-lg border ${toast.type === 'success' ? 'bg-[var(--success-50)] text-[var(--success-600)] border-[var(--success-100)]' : 'bg-[var(--error-50)] text-[var(--error-600)] border-[var(--error-100)]'}`}>
            <span>{toast.message}</span>
            {toast.actionLabel && toast.onAction && (
              <button
                type="button"
                onClick={() => {
                  const action = toast.onAction
                  setToast(null)
                  if (action) action()
                }}
                className="text-xs font-semibold text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
              >
                {toast.actionLabel}
              </button>
            )}
          </div>
        </div>
      )}

      <ConfirmDialog
        open={Boolean(confirmDelete)}
        title="确认删除"
        message="确定要删除这封邮件吗？"
        confirmLabel="删除"
        cancelLabel="取消"
        onConfirm={confirmDeleteEmail}
        onCancel={cancelDeleteEmail}
      />

      {/* Context Menu */}
      {contextMenu && (
        <div
          className="fixed z-50 w-48 rounded-lg border border-[var(--border-default)] bg-[var(--bg-primary)] shadow-xl py-1 text-sm text-[var(--text-secondary)]"
          style={{ top: contextMenu.y, left: contextMenu.x }}
          onClick={closeContextMenu}
        >
          {contextMenuItems.map((item, index) => (
            <button
              key={`${item.label}-${index}`}
              type="button"
              onClick={item.action}
              className={`w-full text-left px-3 py-2 transition-colors ${
                index === contextMenuIndex ? 'bg-[var(--bg-tertiary)]' : 'hover:bg-[var(--bg-tertiary)]'
              } ${item.danger ? 'text-[var(--error-600)] hover:bg-[var(--error-50)]' : ''}`}
            >
              {item.label}
            </button>
          ))}
        </div>
      )}

      {/* Left Sidebar - Email List */}
      <div className="w-96 border-r border-[var(--border-light)] bg-[var(--bg-primary)] flex flex-col min-h-0">
        <div className="p-4 border-b border-[var(--border-light)] bg-[var(--bg-secondary)] flex-shrink-0">
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2 text-[var(--text-primary)]">
              <Mail className="w-4 h-4 text-[var(--text-tertiary)]" />
              <span className="text-sm font-semibold">收件箱</span>
              <span className="text-xs text-[var(--text-tertiary)]">
                ({filteredEmails.length})
              </span>
            </div>
          </div>

          {accounts.length > 0 && (
            <div className="mb-4">
              <div className="mb-2 text-xs text-[var(--text-tertiary)]">账户</div>
              <div className="flex flex-wrap gap-2 max-h-40 overflow-y-auto pr-1 thin-scrollbar">
                {/* All Accounts Button */}
                <Tooltip content="全部账户">
                  <button
                    type="button"
                    onClick={() => setSelectedAccountId(null)}
                    className={`relative flex flex-col items-center justify-center gap-1 px-2 py-2 rounded-lg border text-[10px] font-medium whitespace-nowrap transition-all w-16 ${
                      selectedAccountId === null
                        ? 'bg-[var(--bg-secondary)] text-[var(--text-primary)] border-[var(--border-default)]'
                        : 'bg-[var(--bg-primary)] text-[var(--text-secondary)] border-[var(--border-default)] hover:border-[var(--border-hover)]'
                    }`}
                    style={selectedAccountId === null ? { borderBottomWidth: '2px', borderBottomColor: 'var(--primary-600)' } : undefined}
                  >
                    <span className="flex items-center justify-center w-5 h-5 rounded-full bg-[var(--primary-100)] text-[var(--primary-700)]">
                      <Mail className="w-3.5 h-3.5" />
                    </span>
                    <span className="text-[var(--text-secondary)]">
                      全部
                    </span>
                  </button>
                </Tooltip>

                {/* Account Buttons */}
                {accounts.map(account => {
                  const providerText = providers.find(p => p.value === account.provider)?.label || account.provider
                  const statusText = account.status === 'active' ? '已连接' : '未连接'
                  const syncStatus = syncStatuses[String(account.id)]
                  const isSyncing = syncStatus?.running === true
                  const syncInfo = isSyncing ? '正在同步...' : formatSyncTime(account.last_sync_time)
                  const tip = `${account.email} · ${providerText} · ${statusText} · ${syncInfo}`
                  const isSelected = selectedAccountId === account.id
                  const accountColor = account.color || '#6366f1'
                  return (
                    <Tooltip key={account.id} content={tip}>
                      <button
                        type="button"
                        onClick={() => setSelectedAccountId(account.id)}
                        className={`relative flex flex-col items-center justify-center gap-1 px-2 py-2 rounded-lg border text-[10px] font-medium whitespace-nowrap transition-all w-16 ${
                          isSelected
                            ? 'bg-[var(--bg-secondary)] text-[var(--text-primary)] border-[var(--border-default)]'
                            : 'bg-[var(--bg-primary)] text-[var(--text-secondary)] border-[var(--border-default)] hover:border-[var(--border-hover)]'
                        }`}
                        style={isSelected ? { borderBottomWidth: '2px', borderBottomColor: accountColor } : undefined}
                      >
                        <span 
                          className="flex items-center justify-center w-5 h-5 rounded-full font-semibold text-white"
                          style={{ backgroundColor: accountColor }}
                        >
                          {isSyncing ? (
                            <Loader2 className="w-3 h-3 animate-spin" />
                          ) : (
                            account.email?.[0]?.toUpperCase() || '?'
                          )}
                        </span>
                        <span className="leading-tight text-[var(--text-secondary)]">
                          {account.provider.slice(0, 4)}
                        </span>
                      </button>
                    </Tooltip>
                  )
                })}
              </div>
            </div>
          )}

          {/* Filter Buttons */}
          <div className="flex items-center gap-2 mb-3">
            {(['all', 'unread'] as const).map((f) => (
              <button
                key={f}
                type="button"
                onClick={() => setFilter(f)}
                className={`px-3 py-1.5 text-xs rounded-full border transition-colors ${
                  filter === f
                    ? 'bg-[var(--primary-600)] text-white border-[var(--primary-600)]'
                    : 'bg-[var(--bg-primary)] text-[var(--text-tertiary)] border-[var(--border-default)] hover:border-[var(--border-hover)] hover:text-[var(--text-secondary)]'
                }`}
              >
                {f === 'all' ? '全部' : '未读'}
              </button>
            ))}
          </div>

          {/* Search Input */}
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-[var(--text-tertiary)] pointer-events-none" />
            <input
              type="text"
              placeholder="搜索邮件..."
              value={search}
              onChange={e => setSearch(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && loadEmails()}
              style={{ paddingLeft: '36px' }}
            />
          </div>

          <div className="mt-3 text-[11px] text-[var(--text-tertiary)]">
            快捷键：j/k 上下，o 打开，r 刷新
          </div>
        </div>

        {/* Email List */}
        <div className="flex-1 overflow-auto thin-scrollbar" onScroll={handleScroll}>
          {filteredEmails.length === 0 ? (
            <div className="empty-state">
              <Mail className="empty-state-icon" />
              <p className="text-[var(--text-secondary)]">暂无邮件</p>
              <p className="text-sm mt-1 text-[var(--text-tertiary)]">请先添加邮箱账户</p>
            </div>
          ) : (
            <div className="pt-2 pb-4">
              {sortedFilteredEmails.map(email => {
                const accountColor = getAccountColor(email.account_id)
                const isSelected = selectedEmail?.id === email.id
                return (
                  <div
                    key={email.id}
                    ref={(node) => { emailItemRefs.current[email.id] = node }}
                    onClick={() => handleRead(email)}
                    onContextMenu={(event) => {
                      event.preventDefault()
                      const menuWidth = 192
                      const menuHeight = 132
                      const padding = 8
                      const maxX = window.innerWidth - menuWidth - padding
                      const maxY = window.innerHeight - menuHeight - padding
                      const x = Math.min(event.clientX, maxX)
                      const y = Math.min(event.clientY, maxY)
                      setContextMenu({
                        email,
                        x: Math.max(padding, x),
                        y: Math.max(padding, y),
                      })
                      setContextMenuIndex(0)
                    }}
                    className={`group mx-3 my-1 rounded-xl border cursor-pointer transition-all duration-200 ${
                      isSelected
                        ? 'bg-gradient-to-r from-[var(--primary-50)] to-[var(--bg-primary)] border-[var(--primary-300)] shadow-md ring-1 ring-[var(--primary-200)]'
                        : 'bg-[var(--bg-primary)] border-[var(--border-light)] hover:border-[var(--border-default)] hover:shadow-md hover:-translate-y-0.5'
                    } ${!email.is_read ? 'shadow-sm' : ''}`}
                  >
                    <div className="px-3 py-2.5">
                      <div className="flex items-start gap-2.5">
                        {/* Avatar */}
                        <div
                          className={`w-8 h-8 rounded-full flex items-center justify-center text-xs font-semibold text-white flex-shrink-0 shadow-sm ${!email.is_read ? 'ring-2 ring-offset-1 ring-[var(--primary-200)]' : ''}`}
                          style={{ backgroundColor: accountColor }}
                        >
                          {(email.from_name || email.from)?.[0]?.toUpperCase() || '?'}
                        </div>

                        {/* Content */}
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center gap-1.5">
                            <span className={`text-sm truncate ${!email.is_read ? 'font-semibold text-[var(--text-primary)]' : 'font-medium text-[var(--text-secondary)]'}`}>
                              {email.from_name || email.from}
                            </span>
                            {!email.is_read && (
                              <span className="w-1.5 h-1.5 rounded-full bg-[var(--primary-500)] flex-shrink-0" title="未读" />
                            )}
                          </div>
                          <p className={`text-sm truncate ${!email.is_read ? 'text-[var(--text-primary)] font-medium' : 'text-[var(--text-secondary)]'}`}>
                            {email.subject || '(无主题)'}
                          </p>
                          {email.has_attachment && (
                            <div className="flex items-center gap-1 mt-0.5">
                              <Paperclip className="w-3 h-3 text-[var(--text-tertiary)]" />
                              <span className="text-[10px] text-[var(--text-tertiary)]">附件</span>
                            </div>
                          )}
                        </div>

                        {/* Actions & Date */}
                        <div className="flex flex-col items-end gap-1">
                          <div className="flex items-center gap-0.5 opacity-0 translate-x-2 group-hover:opacity-100 group-hover:translate-x-0 transition-all duration-200">
                            {email.is_read ? (
                              <button
                                type="button"
                                onClick={(event) => {
                                  event.stopPropagation()
                                  handleMarkAsUnread(email)
                                }}
                                className="p-1.5 rounded-md text-[var(--text-tertiary)] hover:text-[var(--primary-600)] hover:bg-[var(--primary-50)] transition-colors"
                                title="标记为未读"
                              >
                                <EyeOff className="w-3.5 h-3.5" />
                              </button>
                            ) : (
                              <button
                                type="button"
                                onClick={(event) => {
                                  event.stopPropagation()
                                  handleRead(email)
                                }}
                                className="p-1.5 rounded-md text-[var(--primary-600)] hover:text-[var(--primary-700)] hover:bg-[var(--primary-50)] transition-colors"
                                title="标记为已读"
                              >
                                <Eye className="w-3.5 h-3.5" />
                              </button>
                            )}
                            <button
                              type="button"
                              onClick={(event) => {
                                event.stopPropagation()
                                requestDeleteEmail(email)
                              }}
                              className="p-1.5 rounded-md text-[var(--text-tertiary)] hover:text-[var(--error-600)] hover:bg-[var(--error-50)] transition-colors"
                              title="删除"
                            >
                              <Trash2 className="w-3.5 h-3.5" />
                            </button>
                          </div>
                          <span className="text-[10px] text-[var(--text-tertiary)] whitespace-nowrap">
                            {formatRelativeTime(resolveEmailDate(email))}
                          </span>
                        </div>
                      </div>
                    </div>
                  </div>
                )
              })}

              {loadingMore && (
                <div className="flex justify-center py-4">
                  <div className="w-5 h-5 border-2 border-[var(--primary-500)] border-t-transparent rounded-full animate-spin" />
                </div>
              )}
              {!hasMore && emails.length > 0 && (
                <div className="text-center py-4 text-sm text-[var(--text-tertiary)]">
                  共 {total} 封邮件
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Right Panel - Email Detail */}
      <div className="flex-1 bg-[var(--bg-primary)] min-h-0">
        {selectedEmail ? (
          <div className="h-full flex flex-col">
            <div className="p-6 border-b border-[var(--border-light)] bg-[var(--bg-secondary)] flex-shrink-0 relative">
              <div className="flex items-start gap-4 pr-16">
                <div className="min-w-0 flex-1">
                  <h2 className="text-xl font-semibold text-[var(--text-primary)] truncate mb-3">
                    {selectedEmail.subject || '(无主题)'}
                  </h2>
                  <div className="flex items-center gap-3">
                    <div className="avatar">
                      {selectedEmail.from_name?.[0] || '?'}
                    </div>
                    <div>
                      <p className="font-medium text-[var(--text-primary)]">{selectedEmail.from_name || selectedEmail.from}</p>
                      <p className="text-sm text-[var(--text-secondary)]">{selectedEmail.from}</p>
                    </div>
                  </div>
                </div>
              </div>
              <div className="absolute top-5 right-6 flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => handleMarkAsUnread(selectedEmail)}
                  className="btn btn-secondary p-2"
                  title="标记未读"
                >
                  <EyeOff className="w-4 h-4" />
                </button>
                <button
                  type="button"
                  onClick={() => requestDeleteEmail(selectedEmail)}
                  className="btn btn-danger p-2"
                  title="删除"
                >
                  <Trash2 className="w-4 h-4" />
                </button>
                <button
                  type="button"
                  onClick={() => handleSendToTelegram(selectedEmail)}
                  disabled={sendingTelegram}
                  className="btn btn-secondary p-2"
                  title="发送到 Telegram"
                >
                  {sendingTelegram ? <Loader2 className="w-4 h-4 animate-spin" /> : <Send className="w-4 h-4" />}
                </button>
              </div>
              <div className="mt-3 text-sm text-[var(--text-tertiary)]">
                {new Date(selectedEmail.date).toLocaleString('zh-CN')}
              </div>
            </div>
            <div ref={detailScrollRef} className="flex-1 p-6 overflow-auto thin-scrollbar text-[var(--text-primary)] leading-relaxed">
              <div className="max-w-3xl mx-auto">
                {selectedEmail.body_html ? (
                  <div className="prose prose-slate max-w-none" dangerouslySetInnerHTML={{ __html: sanitizedHtml }} />
                ) : selectedEmail.body_text ? (
                  <pre className="whitespace-pre-wrap text-[var(--text-primary)] leading-relaxed font-sans">{selectedEmail.body_text}</pre>
                ) : (
                  <p className="text-[var(--text-tertiary)]">无邮件内容</p>
                )}
              </div>
            </div>
          </div>
        ) : (
          <div className="h-full flex items-center justify-center text-[var(--text-tertiary)]">
            <div className="text-center">
              <Mail className="w-16 h-16 mx-auto mb-3 opacity-30" />
              <p>选择一封邮件查看详情</p>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
