import { useState, useEffect, useCallback, useRef, useMemo, memo } from 'react'
import { emailApi, accountApi, syncApi, telegramApi } from '../api'
import axios from 'axios'
import ConfirmDialog from '../components/ConfirmDialog'
import Tooltip from '../components/Tooltip'
import { useTheme } from '../context/ThemeContext'
import type { Email, EmailAccount, SyncStatus } from '../api'
import {
  Search,
  Mail,
  MailOpen,
  Paperclip,
  Trash2,
  EyeOff,
  Loader2,
  Send,
  ArrowLeft,
  RefreshCw,
  X,
  Check,
  ChevronUp,
  ChevronDown,
  Sun,
  Moon,
  Copy,
  Keyboard,
} from 'lucide-react'

const providers = [
  { value: 'gmail', label: 'Gmail' },
  { value: 'qq', label: 'QQ邮箱' },
  { value: 'outlook', label: 'Outlook' },
  { value: 'qq-work', label: 'QQ企业邮' },
  { value: '163', label: '163邮箱' },
  { value: 'custom', label: '自定义' },
]

const EMAIL_ROW_STRIDE = 66
const VIRTUAL_OVERSCAN = 8

interface EmailListRowProps {
  email: Email
  accountColor: string
  dateLabel: string
  isSelected: boolean
  batchMode: boolean
  isChecked: boolean
  registerRef: (id: number, node: HTMLDivElement | null) => void
  onSelect: (email: Email) => void
  onContextMenu: (email: Email, x: number, y: number) => void
  onToggleSelect: (id: number) => void
  onToggleRead: (email: Email) => void
  onRequestDelete: (email: Email) => void
}

const EmailListRow = memo(function EmailListRow({
  email,
  accountColor,
  dateLabel,
  isSelected,
  batchMode,
  isChecked,
  registerRef,
  onSelect,
  onContextMenu,
  onToggleSelect,
  onToggleRead,
  onRequestDelete,
}: EmailListRowProps) {
  return (
    <div
      ref={(node) => registerRef(email.id, node)}
      onClick={() => onSelect(email)}
      onContextMenu={(event) => {
        event.preventDefault()
        const menuWidth = 176
        const menuHeight = 160
        const padding = 8
        const x = Math.min(event.clientX, window.innerWidth - menuWidth - padding)
        const y = Math.min(event.clientY, window.innerHeight - menuHeight - padding)
        onContextMenu(email, Math.max(padding, x), Math.max(padding, y))
      }}
      style={isSelected ? { boxShadow: 'var(--active-highlight)', height: '64px', marginBottom: '2px' } : { height: '64px', marginBottom: '2px' }}
      className={`group px-3 py-2.5 rounded-[8px] border cursor-pointer transition-colors ${
        isSelected
          ? 'bg-[var(--bg-tertiary)] border-[var(--border-default)]'
          : 'border-transparent hover:bg-[var(--bg-nav-hover)]'
      }`}
    >
      <div className="flex items-start gap-2.5">
        {batchMode ? (
          <input
            type="checkbox"
            checked={isChecked}
            onClick={(e) => e.stopPropagation()}
            onChange={(e) => {
              e.stopPropagation()
              onToggleSelect(email.id)
            }}
            className="mt-0.5 w-4 h-4 rounded border-[var(--border-default)] accent-[var(--primary-500)] cursor-pointer"
          />
        ) : (
          <span className="flex w-2.5 pt-2 justify-center flex-shrink-0">
            {!email.is_read && (
              <span
                className="w-1.5 h-1.5 rounded-full bg-[var(--primary-500)]"
                title="未读"
              />
            )}
          </span>
        )}

        <div className="flex-1 min-w-0">
          <div className="flex h-5 items-center justify-between gap-2">
            <div className="flex items-center gap-1.5 min-w-0">
              <span
                className="w-[3px] h-3 rounded-full flex-shrink-0"
                style={{ backgroundColor: accountColor }}
              />
              <span
                className={`text-sm truncate ${
                  !email.is_read
                    ? 'font-medium text-[var(--text-primary)]'
                    : 'font-normal text-[var(--text-secondary)]'
                }`}
              >
                {email.from_name || email.from}
              </span>
            </div>

            <div className="flex items-center flex-shrink-0">
              <div className="hidden md:group-hover:flex items-center gap-0.5">
                <button
                  type="button"
                  onClick={(event) => {
                    event.stopPropagation()
                    onToggleRead(email)
                  }}
                  className="p-0.5 rounded-[4px] text-[var(--text-tertiary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-tertiary)] cursor-pointer"
                  title={email.is_read ? '标为未读' : '标为已读'}
                >
                  {email.is_read ? (
                    <EyeOff className="w-3.5 h-3.5" strokeWidth={1.5} />
                  ) : (
                    <MailOpen className="w-3.5 h-3.5" strokeWidth={1.5} />
                  )}
                </button>
                <button
                  type="button"
                  onClick={(event) => {
                    event.stopPropagation()
                    onRequestDelete(email)
                  }}
                  className="p-0.5 rounded-[4px] text-[var(--text-tertiary)] hover:text-[var(--error-600)] hover:bg-[var(--error-50)] cursor-pointer"
                  title="删除"
                >
                  <Trash2 className="w-3.5 h-3.5" strokeWidth={1.5} />
                </button>
              </div>
              <span className="font-mono tabular-nums text-xs text-[var(--text-tertiary)] whitespace-nowrap md:group-hover:hidden">
                {dateLabel}
              </span>
            </div>
          </div>

          <div className="mt-0.5 flex items-center gap-1.5">
            <p
              className={`text-sm truncate flex-1 ${
                !email.is_read
                  ? 'text-[var(--text-primary)] font-medium'
                  : 'text-[var(--text-secondary)]'
              }`}
            >
              {email.subject || '(无主题)'}
            </p>
            {email.has_attachment && (
              <Paperclip className="w-3.5 h-3.5 text-[var(--text-tertiary)] flex-shrink-0" strokeWidth={1.5} />
            )}
          </div>
        </div>
      </div>
    </div>
  )
})

export default function Dashboard() {
  const { resolvedTheme } = useTheme()
  const [emails, setEmails] = useState<Email[]>([])
  const [accounts, setAccounts] = useState<EmailAccount[]>([])
  const [selectedAccountId, setSelectedAccountId] = useState<number | null>(null)
  const [selectedEmail, setSelectedEmail] = useState<Email | null>(null)
  const [search, setSearch] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [filter, setFilter] = useState<'all' | 'unread'>('all')
  const [syncStatuses, setSyncStatuses] = useState<Record<string, SyncStatus>>({})
  const [syncing, setSyncing] = useState(false)
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
  const syncPollTimer = useRef<number | null>(null)
  const [pendingDeletes, setPendingDeletes] = useState<Record<number, Email>>({})
  const [contextMenu, setContextMenu] = useState<{
    email: Email
    x: number
    y: number
  } | null>(null)
  const [contextMenuIndex, setContextMenuIndex] = useState(0)
  const emailItemRefs = useRef<Record<number, HTMLDivElement | null>>({})
  const listScrollRef = useRef<HTMLDivElement | null>(null)
  const detailScrollRef = useRef<HTMLDivElement | null>(null)
  const searchInputRef = useRef<HTMLInputElement | null>(null)
  const listViewportHeightRef = useRef(600)
  const listScrollTopRef = useRef(0)
  const [virtualWindow, setVirtualWindow] = useState<{ start: number; end: number }>({ start: 0, end: 30 })
  const [confirmDelete, setConfirmDelete] = useState<{ email: Email } | null>(null)
  const [confirmBatchDelete, setConfirmBatchDelete] = useState(false)
  const [sendingTelegram, setSendingTelegram] = useState(false)
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set())
  const [batchMode, setBatchMode] = useState(false)
  const [loadingDetail, setLoadingDetail] = useState(false)
  const [paperLight, setPaperLight] = useState(true)
  const [showShortcuts, setShowShortcuts] = useState(false)

  const formatSyncTime = (value?: string) => {
    if (!value) return '未同步'
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) return '未同步'
    const diffMinutes = Math.floor((Date.now() - date.getTime()) / 60000)
    if (diffMinutes < 1) return '刚刚同步'
    if (diffMinutes < 60) return `${diffMinutes} 分钟前`
    const diffHours = Math.floor(diffMinutes / 60)
    if (diffHours < 24) return `${diffHours} 小时前`
    const diffDays = Math.floor(diffHours / 24)
    if (diffDays < 7) return `${diffDays} 天前`
    return date.toLocaleDateString('zh-CN')
  }

  const formatRelativeTime = (date: Date | null) => {
    if (!date) return '未知'
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    if (diffMs < 0) return date.toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' })

    const diffMinutes = Math.floor(diffMs / 60000)
    if (diffMinutes < 1) return '刚刚'
    if (diffMinutes < 60) return `${diffMinutes}分`

    const diffHours = Math.floor(diffMinutes / 60)
    if (diffHours < 24) return `${diffHours}时`

    const diffDays = Math.floor(diffHours / 24)
    if (diffDays === 1) return '昨天'
    if (diffDays < 7) return `${diffDays}天`

    if (date.getFullYear() === now.getFullYear()) {
      return `${date.getMonth() + 1}/${date.getDate()}`
    }
    return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`
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
      return res.data
    } catch (e) {
      console.error(e)
      return null
    }
  }, [])

  const loadingMoreRef = useRef(false)
  const hasMoreRef = useRef(true)
  const nextCursorRef = useRef<{ date: string; id: number } | null>(null)
  const listAbortRef = useRef<AbortController | null>(null)
  const activeEmailIdRef = useRef<number | null>(null)
  const detailCacheRef = useRef<Map<number, { email: Email; sanitizedHtml: string }>>(new Map())

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setDebouncedSearch(search)
    }, 300)
    return () => window.clearTimeout(timer)
  }, [search])

  const loadEmails = useCallback(async (append: boolean = false) => {
    if (append && (loadingMoreRef.current || !hasMoreRef.current)) return

    if (!append) {
      listAbortRef.current?.abort()
    }
    const controller = new AbortController()
    if (!append) {
      listAbortRef.current = controller
    }

    loadingMoreRef.current = true
    setLoadingMore(true)

    try {
      const cursor = append ? nextCursorRef.current : null
      const res = await emailApi.list(
        {
          page: 1,
          page_size: 50,
          search: debouncedSearch || undefined,
          account_id: selectedAccountId || undefined,
          unread: filter === 'unread' ? true : undefined,
          cursor_date: cursor?.date,
          cursor_id: cursor?.id,
        },
        controller.signal,
      )

      if (append) {
        setEmails(prev => {
          const existingIds = new Set(prev.map(item => item.id))
          const incoming = res.data.filter(item => !existingIds.has(item.id))
          return [...prev, ...incoming]
        })
      } else {
        setEmails(res.data)
        setTotal(res.total)
      }

      nextCursorRef.current = res.next_cursor ?? null
      const hasMoreVal = Boolean(res.has_more)
      hasMoreRef.current = hasMoreVal
      setHasMore(hasMoreVal)
    } catch (e) {
      if (!axios.isCancel(e)) {
        console.error(e)
      }
    } finally {
      loadingMoreRef.current = false
      setLoadingMore(false)
    }
  }, [debouncedSearch, selectedAccountId, filter])

  const syncVirtualWindow = useCallback((totalRows: number) => {
    if (totalRows === 0) {
      setVirtualWindow(prev => (prev.start === 0 && prev.end === 0 ? prev : { start: 0, end: 0 }))
      return
    }
    const start = Math.max(0, Math.floor(listScrollTopRef.current / EMAIL_ROW_STRIDE) - VIRTUAL_OVERSCAN)
    const visibleCount = Math.ceil(listViewportHeightRef.current / EMAIL_ROW_STRIDE) + VIRTUAL_OVERSCAN * 2
    const end = Math.min(totalRows, start + visibleCount)
    setVirtualWindow(prev => (prev.start === start && prev.end === end ? prev : { start, end }))
  }, [])

  useEffect(() => {
    const el = listScrollRef.current
    if (!el) return
    listViewportHeightRef.current = el.clientHeight || 600
    syncVirtualWindow(emails.length)
    const observer = new ResizeObserver(entries => {
      for (const entry of entries) {
        if (entry.contentRect.height > 0) {
          listViewportHeightRef.current = entry.contentRect.height
          syncVirtualWindow(emails.length)
        }
      }
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [emails.length, syncVirtualWindow])

  const handleScroll = useCallback((e: React.UIEvent<HTMLDivElement>) => {
    const target = e.target as HTMLDivElement
    const { scrollTop, scrollHeight, clientHeight } = target
    listScrollTopRef.current = scrollTop
    syncVirtualWindow(emails.length)
    if (scrollHeight - scrollTop - clientHeight < 240 && !loadingMore && hasMore) {
      loadEmails(true)
    }
  }, [emails.length, syncVirtualWindow, loadEmails, hasMore, loadingMore])

  useEffect(() => {
    loadAccounts()
    loadSyncStatuses()
    return () => {
      listAbortRef.current?.abort()
      if (syncPollTimer.current) {
        window.clearInterval(syncPollTimer.current)
      }
    }
  }, [loadAccounts, loadSyncStatuses])

  useEffect(() => {
    setTotal(0)
    nextCursorRef.current = null
    hasMoreRef.current = true
    setHasMore(true)
    if (listScrollRef.current) {
      listScrollRef.current.scrollTop = 0
    }
    listScrollTopRef.current = 0
    syncVirtualWindow(0)
    loadEmails(false)
  }, [debouncedSearch, selectedAccountId, filter, loadEmails, syncVirtualWindow])
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
    }, 2200)
  }, [])

  const parseEmailDate = useCallback((value?: string | null) => {
    if (!value) return null
    const direct = new Date(value)
    if (!Number.isNaN(direct.getTime())) return direct

    const goTimeMatch = value.match(/^(\d{4}-\d{2}-\d{2})[ T](\d{2}:\d{2}:\d{2})(?:\.\d+)?\s([+-]\d{4})/)
    if (goTimeMatch) {
      const [, datePart, timePart, offset] = goTimeMatch
      const offsetWithColon = offset.replace(/([+-]\d{2})(\d{2})/, '$1:$2')
      const parsed = new Date(`${datePart}T${timePart}${offsetWithColon}`)
      if (!Number.isNaN(parsed.getTime())) return parsed
    }

    const localParsed = new Date(value.replace(' ', 'T'))
    if (!Number.isNaN(localParsed.getTime())) return localParsed

    return null
  }, [])

  const resolveEmailDate = useCallback((email: Email) => {
    const primary = email.date || email.created_at
    const fallback = email.created_at || email.date
    const primaryDate = parseEmailDate(primary)
    if (primaryDate) return primaryDate
    return parseEmailDate(fallback)
  }, [parseEmailDate])

  const handleQuickSync = useCallback(async () => {
    if (syncing || accounts.length === 0) return
    setSyncing(true)
    try {
      const targetIds = selectedAccountId
        ? [selectedAccountId]
        : accounts.filter(a => a.status === 'active').map(a => a.id)
      const idsToSync = targetIds.length > 0 ? targetIds : accounts.map(a => a.id)
      await Promise.allSettled(idsToSync.map(id => accountApi.sync(id)))
      showToast('正在同步邮件', 'success')
      await loadSyncStatuses()

      if (syncPollTimer.current) {
        window.clearInterval(syncPollTimer.current)
      }
      let attempts = 0
      syncPollTimer.current = window.setInterval(async () => {
        attempts += 1
        const statuses = await loadSyncStatuses()
        const stillRunning = statuses && idsToSync.some(id => statuses[String(id)]?.running)
        if (!stillRunning || attempts >= 20) {
          if (syncPollTimer.current) {
            window.clearInterval(syncPollTimer.current)
            syncPollTimer.current = null
          }
          setSyncing(false)
          await Promise.all([loadEmails(false), loadAccounts()])
        }
      }, 1500)
    } catch (e) {
      console.error(e)
      showToast('同步启动失败', 'error')
      setSyncing(false)
    }
  }, [syncing, accounts, selectedAccountId, showToast, loadSyncStatuses, loadEmails, loadAccounts])

  const sanitizeEmailHtml = useCallback((rawHtml?: string) => {
    if (!rawHtml) return ''
    try {
      const parser = new DOMParser()
      const doc = parser.parseFromString(rawHtml, 'text/html')
      doc.querySelectorAll('style, link[rel="stylesheet"], script').forEach(node => node.remove())
      doc.querySelectorAll('img').forEach(img => {
        img.setAttribute('loading', 'lazy')
      })
      doc.querySelectorAll('a').forEach(link => {
        link.setAttribute('target', '_blank')
        link.setAttribute('rel', 'noopener noreferrer')
      })
      return doc.body?.innerHTML || ''
    } catch (e) {
      console.error(e)
      return ''
    }
  }, [])

  const handleRead = useCallback(async (email: Email, shouldScroll = false, scrollDetail = false) => {
    activeEmailIdRef.current = email.id
    const wasUnread = !email.is_read

    if (scrollDetail && detailScrollRef.current) {
      detailScrollRef.current.scrollTo({ top: 0, behavior: 'smooth' })
    }

    if (wasUnread) {
      setEmails(prev => prev.map(e => e.id === email.id ? { ...e, is_read: true } : e))
    }

    const cached = detailCacheRef.current.get(email.id)
    if (cached) {
      // LRU refresh
      detailCacheRef.current.delete(email.id)
      const updated = wasUnread ? { ...cached.email, is_read: true } : cached.email
      detailCacheRef.current.set(email.id, { ...cached, email: updated })
      setSelectedEmail(updated)
      setLoadingDetail(false)
      if (wasUnread) {
        emailApi.markAsRead(email.id).catch(console.error)
      }
      return
    }

    setSelectedEmail(wasUnread ? { ...email, is_read: true } : email)
    setLoadingDetail(true)

    if (shouldScroll) {
      emailItemRefs.current[email.id]?.scrollIntoView({ block: 'nearest' })
    }

    emailApi.get(email.id).then(detail => {
      const fullEmail = wasUnread ? { ...detail.data, is_read: true } : detail.data
      const cleaned = sanitizeEmailHtml(fullEmail.body_html)
      if (detailCacheRef.current.size >= 100) {
        const oldestKey = detailCacheRef.current.keys().next().value
        if (oldestKey !== undefined) {
          detailCacheRef.current.delete(oldestKey)
        }
      }
      detailCacheRef.current.set(email.id, { email: fullEmail, sanitizedHtml: cleaned })
      if (activeEmailIdRef.current === email.id) {
        setSelectedEmail(fullEmail)
        setLoadingDetail(false)
      }
      if (wasUnread) {
        emailApi.markAsRead(email.id).catch(console.error)
      }
    }).catch((e: unknown) => {
      console.error('Failed to fetch email detail:', e)
      if (activeEmailIdRef.current === email.id) {
        setLoadingDetail(false)
      }
      if (wasUnread) {
        emailApi.markAsRead(email.id).catch(console.error)
      }
    })
  }, [sanitizeEmailHtml])

  const handleMarkAsUnread = useCallback(async (email: Email) => {
    if (!email.is_read) return
    try {
      await emailApi.markAsUnread(email.id)
      setEmails(prev => prev.map(e => e.id === email.id ? { ...e, is_read: false } : e))
      if (selectedEmail?.id === email.id) {
        setSelectedEmail({ ...email, is_read: false })
      }
      showToast('已标为未读', 'success')
    } catch (e) {
      console.error(e)
      showToast('操作失败', 'error')
    }
  }, [selectedEmail, showToast])

  const handleToggleReadRow = useCallback(async (email: Email) => {
    if (email.is_read) {
      await handleMarkAsUnread(email)
    } else {
      try {
        await emailApi.markAsRead(email.id)
        setEmails(prev => prev.map(e => e.id === email.id ? { ...e, is_read: true } : e))
        if (selectedEmail?.id === email.id) {
          setSelectedEmail({ ...email, is_read: true })
        }
        showToast('已标为已读', 'success')
      } catch (e) {
        console.error(e)
        showToast('操作失败', 'error')
      }
    }
  }, [handleMarkAsUnread, selectedEmail, showToast])

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
        detailCacheRef.current.delete(email.id)
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
  }, [pendingDeletes, selectedEmail, showToast])

  const confirmDeleteEmail = useCallback(() => {
    if (!confirmDelete) return
    handleDeleteEmail(confirmDelete.email)
    setConfirmDelete(null)
  }, [confirmDelete, handleDeleteEmail])

  const handleSendToTelegram = useCallback(async (email: Email) => {
    setSendingTelegram(true)
    try {
      await telegramApi.sendEmail(email.id)
      showToast('已转发到 Telegram', 'success')
    } catch (e) {
      const message = axios.isAxiosError(e) ? e.response?.data?.error : undefined
      showToast(message || '转发失败', 'error')
    } finally {
      setSendingTelegram(false)
    }
  }, [showToast])

  const handleBatchMarkAsRead = useCallback(async () => {
    const ids = Array.from(selectedIds)
    if (ids.length === 0) return
    try {
      await emailApi.batchMarkAsRead(ids)
      const idSet = new Set(ids)
      setEmails(prev => prev.map(e => idSet.has(e.id) ? { ...e, is_read: true } : e))
      setSelectedIds(new Set())
      showToast(`已将 ${ids.length} 封标为已读`, 'success')
    } catch {
      showToast('批量操作失败', 'error')
    }
  }, [selectedIds, showToast])

  const handleBatchDelete = useCallback(async () => {
    const ids = Array.from(selectedIds)
    setConfirmBatchDelete(false)
    if (ids.length === 0) return
    try {
      const results = await Promise.allSettled(ids.map(id => emailApi.delete(id)))
      const deletedIds = new Set(ids.filter((_, index) => results[index].status === 'fulfilled'))
      deletedIds.forEach(id => detailCacheRef.current.delete(id))
      setEmails(prev => prev.filter(e => !deletedIds.has(e.id)))
      if (selectedEmail && deletedIds.has(selectedEmail.id)) {
        setSelectedEmail(null)
      }
      setSelectedIds(new Set())
      showToast(`已删除 ${deletedIds.size} 封邮件`, 'success')
    } catch {
      showToast('批量删除失败', 'error')
    }
  }, [selectedIds, selectedEmail, showToast])

  const toggleSelect = useCallback((id: number) => {
    setSelectedIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const filteredEmails = emails

  const toggleSelectAll = useCallback(() => {
    if (selectedIds.size === filteredEmails.length) {
      setSelectedIds(new Set())
    } else {
      setSelectedIds(new Set(filteredEmails.map(e => e.id)))
    }
  }, [selectedIds.size, filteredEmails])

  const toggleInvert = useCallback(() => {
    const newSelected = new Set<number>()
    filteredEmails.forEach(e => {
      if (!selectedIds.has(e.id)) {
        newSelected.add(e.id)
      }
    })
    setSelectedIds(newSelected)
  }, [selectedIds, filteredEmails])

  // 预处理每封邮件的时间戳与相对时间字符串，避免排序和滚动渲染期间重复正则/Date 解析
  const emailDateMeta = useMemo(() => {
    const map = new Map<number, { ts: number; label: string }>()
    for (const email of filteredEmails) {
      const parsed = resolveEmailDate(email)
      map.set(email.id, {
        ts: parsed ? parsed.getTime() : 0,
        label: formatRelativeTime(parsed),
      })
    }
    return map
  }, [filteredEmails, resolveEmailDate])

  const sortedFilteredEmails = useMemo(() =>
    [...filteredEmails].sort((a, b) => {
      const tsA = emailDateMeta.get(a.id)?.ts ?? 0
      const tsB = emailDateMeta.get(b.id)?.ts ?? 0
      if (tsB !== tsA) return tsB - tsA
      return b.id - a.id
    }),
    [filteredEmails, emailDateMeta],
  )

  const virtualRange = useMemo(() => {
    const totalRows = sortedFilteredEmails.length
    if (totalRows === 0) {
      return { start: 0, end: 0, paddingTop: 0, paddingBottom: 0 }
    }
    const start = Math.min(virtualWindow.start, totalRows)
    const end = Math.min(virtualWindow.end, totalRows)
    return {
      start,
      end,
      paddingTop: start * EMAIL_ROW_STRIDE,
      paddingBottom: Math.max(0, (totalRows - end) * EMAIL_ROW_STRIDE),
    }
  }, [sortedFilteredEmails.length, virtualWindow.start, virtualWindow.end])

  const visibleEmails = useMemo(
    () => sortedFilteredEmails.slice(virtualRange.start, virtualRange.end),
    [sortedFilteredEmails, virtualRange.start, virtualRange.end],
  )

  const registerEmailRef = useCallback((id: number, node: HTMLDivElement | null) => {
    if (node) {
      emailItemRefs.current[id] = node
    } else {
      delete emailItemRefs.current[id]
    }
  }, [])

  const handleRowContextMenu = useCallback((email: Email, x: number, y: number) => {
    setContextMenu({ email, x, y })
    setContextMenuIndex(0)
  }, [])

  const handleRequestDeleteRow = useCallback((email: Email) => {
    setConfirmDelete({ email })
  }, [])
  const scrollToEmailIndex = useCallback((idx: number) => {
    const container = listScrollRef.current
    if (!container || idx < 0) return
    const itemTop = idx * EMAIL_ROW_STRIDE
    const itemBottom = itemTop + EMAIL_ROW_STRIDE
    if (itemTop < container.scrollTop) {
      container.scrollTop = itemTop
    } else if (itemBottom > container.scrollTop + container.clientHeight) {
      container.scrollTop = itemBottom - container.clientHeight
    }
  }, [])

  const selectedIndex = useMemo(() => {
    if (!selectedEmail) return -1
    return sortedFilteredEmails.findIndex(email => email.id === selectedEmail.id)
  }, [selectedEmail, sortedFilteredEmails])

  const stepEmail = useCallback((delta: number) => {
    if (sortedFilteredEmails.length === 0) return
    const nextIndex = selectedIndex < 0
      ? 0
      : Math.max(0, Math.min(sortedFilteredEmails.length - 1, selectedIndex + delta))
    const target = sortedFilteredEmails[nextIndex]
    if (target) {
      scrollToEmailIndex(nextIndex)
      handleRead(target, true, true)
    }
  }, [sortedFilteredEmails, selectedIndex, scrollToEmailIndex, handleRead])
  const unreadCount = useMemo(() => emails.filter(e => !e.is_read).length, [emails])

  const accountById = useMemo(() => {
    const map = new Map<number, EmailAccount>()
    accounts.forEach(a => map.set(a.id, a))
    return map
  }, [accounts])

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (contextMenu || confirmDelete || confirmBatchDelete) return
      const target = event.target as HTMLElement | null
      if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) {
        if (event.key === 'Escape' && target === searchInputRef.current) {
          searchInputRef.current?.blur()
        }
        return
      }

      if (event.key === '?') {
        event.preventDefault()
        setShowShortcuts(prev => !prev)
        return
      }

      if (showShortcuts) {
        if (event.key === 'Escape') setShowShortcuts(false)
        return
      }

      if (event.key === '/') {
        event.preventDefault()
        searchInputRef.current?.focus()
        return
      }

      if (event.key === 'j') {
        event.preventDefault()
        const index = sortedFilteredEmails.findIndex(email => email.id === selectedEmail?.id)
        const nextIdx = index + 1 < sortedFilteredEmails.length ? index + 1 : 0
        const nextEmail = sortedFilteredEmails[nextIdx]
        if (nextEmail) {
          scrollToEmailIndex(nextIdx)
          handleRead(nextEmail, true)
        }
      } else if (event.key === 'k') {
        event.preventDefault()
        const index = sortedFilteredEmails.findIndex(email => email.id === selectedEmail?.id)
        const prevIdx = index <= 0 ? sortedFilteredEmails.length - 1 : index - 1
        const prevEmail = sortedFilteredEmails[prevIdx]
        if (prevEmail) {
          scrollToEmailIndex(prevIdx)
          handleRead(prevEmail, true)
        }
      } else if (event.key === 'o' || event.key === 'O') {
        event.preventDefault()
        const targetEmail = selectedEmail || sortedFilteredEmails[0]
        if (targetEmail) handleRead(targetEmail, true, true)
      } else if (event.key === 'u' || event.key === 'U') {
        if (selectedEmail && selectedEmail.is_read) {
          event.preventDefault()
          handleMarkAsUnread(selectedEmail)
        }
      } else if (event.key === 't' || event.key === 'T') {
        if (selectedEmail && !sendingTelegram) {
          event.preventDefault()
          handleSendToTelegram(selectedEmail)
        }
      } else if (event.key === 'r' || event.key === 'R') {
        event.preventDefault()
        loadEmails(false).then(() => showToast('已刷新', 'success'))
      } else if ((event.key === 'Backspace' || event.key === 'Delete') && selectedEmail) {
        event.preventDefault()
        setConfirmDelete({ email: selectedEmail })
      } else if (event.key === 'Escape' && selectedEmail) {
        setSelectedEmail(null)
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [
    loadEmails,
    handleRead,
    scrollToEmailIndex,
    handleMarkAsUnread,
    handleSendToTelegram,
    sendingTelegram,
    selectedEmail,
    sortedFilteredEmails,
    contextMenu,
    confirmDelete,
    confirmBatchDelete,
    showShortcuts,
    showToast,
  ])

  const contextMenuItems = useMemo(() => {
    if (!contextMenu) return []
    const items: Array<{ label: string; action: () => void | Promise<void>; danger?: boolean }> = [
      { label: '打开', action: () => handleRead(contextMenu.email) },
    ]
    if (contextMenu.email.is_read) {
      items.push({ label: '标为未读', action: () => handleMarkAsUnread(contextMenu.email) })
    } else {
      items.push({ label: '标为已读', action: () => handleRead(contextMenu.email) })
    }
    items.push({ label: '转发到 Telegram', action: () => handleSendToTelegram(contextMenu.email) })
    items.push({ label: '删除', action: () => setConfirmDelete({ email: contextMenu.email }), danger: true })
    return items
  }, [contextMenu, handleMarkAsUnread, handleRead, handleSendToTelegram])

  const sanitizedHtml = useMemo(() => {
    if (!selectedEmail?.body_html) return ''
    const cached = detailCacheRef.current.get(selectedEmail.id)
    if (cached && cached.email.body_html === selectedEmail.body_html) {
      return cached.sanitizedHtml
    }
    return sanitizeEmailHtml(selectedEmail.body_html)
  }, [selectedEmail?.id, selectedEmail?.body_html, sanitizeEmailHtml])

  useEffect(() => {
    if (!contextMenu) return
    const handleMenuKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'ArrowDown') {
        event.preventDefault()
        setContextMenuIndex(prev => (prev + 1) % contextMenuItems.length)
      } else if (event.key === 'ArrowUp') {
        event.preventDefault()
        setContextMenuIndex(prev => (prev - 1 + contextMenuItems.length) % contextMenuItems.length)
      } else if (event.key === 'Enter') {
        event.preventDefault()
        const item = contextMenuItems[contextMenuIndex]
        if (item) item.action()
        setContextMenu(null)
        setContextMenuIndex(0)
      } else if (event.key === 'Escape') {
        event.preventDefault()
        setContextMenu(null)
        setContextMenuIndex(0)
      }
    }
    window.addEventListener('keydown', handleMenuKeyDown)
    return () => window.removeEventListener('keydown', handleMenuKeyDown)
  }, [contextMenu, contextMenuIndex, contextMenuItems])

  useEffect(() => {
    return () => {
      Object.values(deleteTimers.current).forEach(timer => window.clearTimeout(timer))
      deleteTimers.current = {}
    }
  }, [])

  useEffect(() => {
    if (!contextMenu) return
    const close = () => setContextMenu(null)
    window.addEventListener('click', close)
    window.addEventListener('contextmenu', close)
    return () => {
      window.removeEventListener('click', close)
      window.removeEventListener('contextmenu', close)
    }
  }, [contextMenu])

  const selectedEmailAccount = selectedEmail ? accountById.get(selectedEmail.account_id) : undefined
  const useLightPaper = Boolean(selectedEmail?.body_html) && (resolvedTheme === 'light' || paperLight)

  return (
    <div className="h-full flex flex-col md:flex-row bg-[var(--bg-secondary)] overflow-hidden">
      {/* Toast */}
      {toast && (
        <div className="fixed left-4 right-4 top-4 z-50 md:left-auto md:right-4 animate-in fade-in slide-in-from-top-2 duration-200">
          <div
            className={`flex items-center gap-3 px-4 py-2.5 rounded-[8px] text-sm border shadow-[var(--shadow-md)] ${
              toast.type === 'success'
                ? 'bg-[var(--bg-primary)] text-[var(--text-primary)] border-[var(--border-default)]'
                : 'bg-[var(--error-50)] text-[var(--error-700)] border-[var(--error-100)]'
            }`}
          >
            <span
              className={`status-dot ${toast.type === 'success' ? 'status-dot-success' : 'status-dot-error'}`}
            />
            <span>{toast.message}</span>
            {toast.actionLabel && toast.onAction && (
              <button
                type="button"
                onClick={() => {
                  const action = toast.onAction
                  setToast(null)
                  if (action) action()
                }}
                className="ml-2 text-xs font-medium text-[var(--primary-600)] hover:underline cursor-pointer"
              >
                {toast.actionLabel}
              </button>
            )}
          </div>
        </div>
      )}

      <ConfirmDialog
        open={Boolean(confirmDelete)}
        title="删除邮件"
        message="确定要删除这封邮件吗？"
        confirmLabel="删除"
        cancelLabel="取消"
        onConfirm={confirmDeleteEmail}
        onCancel={() => setConfirmDelete(null)}
      />

      <ConfirmDialog
        open={confirmBatchDelete}
        title="批量删除邮件"
        message={`确定要删除选中的 ${selectedIds.size} 封邮件吗？此操作不可撤销。`}
        confirmLabel="删除"
        cancelLabel="取消"
        onConfirm={handleBatchDelete}
        onCancel={() => setConfirmBatchDelete(false)}
      />

      {/* Keyboard Shortcuts Modal */}
      {showShortcuts && (
        <div
          className="overlay-backdrop fixed inset-0 z-50 flex items-center justify-center p-4"
          onClick={() => setShowShortcuts(false)}
        >
          <div
            role="dialog"
            aria-modal="true"
            className="w-full max-w-sm rounded-xl border border-[var(--border-default)] bg-[var(--bg-primary)] p-5 shadow-[var(--shadow-lg)]"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between pb-3 mb-3 border-b border-[var(--border-light)]">
              <div className="flex items-center gap-2 text-sm font-medium text-[var(--text-primary)]">
                <Keyboard className="w-4 h-4 text-[var(--text-tertiary)]" strokeWidth={1.5} />
                <span>键盘快捷键</span>
              </div>
              <button
                type="button"
                onClick={() => setShowShortcuts(false)}
                className="btn btn-ghost w-7 h-7 p-0"
                aria-label="关闭"
              >
                <X className="w-4 h-4" strokeWidth={1.5} />
              </button>
            </div>
            <div className="space-y-2 text-xs text-[var(--text-secondary)]">
              {[
                { keys: ['j', 'k'], desc: '下一封 / 上一封邮件' },
                { keys: ['o'], desc: '打开并滚回顶部' },
                { keys: ['/'], desc: '聚焦搜索框' },
                { keys: ['u'], desc: '标记为未读' },
                { keys: ['t'], desc: '转发到 Telegram' },
                { keys: ['r'], desc: '刷新邮件列表' },
                { keys: ['⌫'], desc: '删除当前邮件' },
                { keys: ['Esc'], desc: '关闭弹窗或返回列表' },
              ].map((item) => (
                <div key={item.desc} className="flex items-center justify-between py-1">
                  <span>{item.desc}</span>
                  <div className="flex items-center gap-1">
                    {item.keys.map((key) => (
                      <kbd key={key} className="kbd">{key}</kbd>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Context Menu */}
      {contextMenu && (
        <div
          className="fixed z-50 w-44 rounded-[10px] border border-[var(--border-default)] bg-[var(--bg-primary)] shadow-[var(--shadow-lg)] p-1 text-sm text-[var(--text-secondary)] animate-in fade-in duration-200"
          style={{ top: contextMenu.y, left: contextMenu.x }}
          onClick={() => {
            setContextMenu(null)
            setContextMenuIndex(0)
          }}
        >
          {contextMenuItems.map((item, index) => (
            <button
              key={`${item.label}-${index}`}
              type="button"
              onClick={item.action}
              className={`w-full text-left px-3 py-1.5 rounded-[6px] transition-colors cursor-pointer ${
                index === contextMenuIndex ? 'bg-[var(--bg-tertiary)] text-[var(--text-primary)]' : 'hover:bg-[var(--bg-tertiary)]'
              } ${item.danger ? 'text-[var(--error-600)] hover:bg-[var(--error-50)]' : ''}`}
            >
              {item.label}
            </button>
          ))}
        </div>
      )}

      {/* Left Column - Mail List */}
      <div className={`${selectedEmail ? 'hidden md:flex' : 'flex'} w-full md:w-[380px] lg:w-[400px] flex-shrink-0 border-r border-[var(--border-light)] bg-[var(--bg-primary)] flex-col min-h-0`}>
        {/* Top Header Bar (aligns with 56px sidebar & detail toolbar) */}
        <div className="h-11 md:h-14 px-4 border-b border-[var(--border-light)] flex items-center justify-between gap-2 flex-shrink-0">
          <div className="flex items-baseline gap-2">
            <h2 className="hidden md:block text-base font-medium text-[var(--text-primary)]">收件箱</h2>
            <span className="font-mono tabular-nums text-xs text-[var(--text-tertiary)]">
              {filteredEmails.length} 封{unreadCount > 0 ? ` · ${unreadCount} 未读` : ''}
            </span>
          </div>
          <Tooltip content="立即同步邮件">
            <button
              type="button"
              onClick={handleQuickSync}
              disabled={syncing || accounts.length === 0}
              className="btn btn-ghost btn-sm px-2 text-xs text-[var(--text-secondary)]"
            >
              <RefreshCw className={`w-3.5 h-3.5 ${syncing ? 'animate-spin' : ''}`} strokeWidth={1.5} />
              <span>{syncing ? '同步中' : '同步'}</span>
            </button>
          </Tooltip>
        </div>

        {/* Search & Filter Controls */}
        <div className="px-4 py-3 border-b border-[var(--border-light)] space-y-2.5 flex-shrink-0">
          {/* Search */}
          <div className="relative flex items-center">
            <Search className="absolute left-3 w-4 h-4 text-[var(--text-tertiary)] pointer-events-none" strokeWidth={1.5} />
            <input
              ref={searchInputRef}
              type="text"
              placeholder="搜索发件人或主题"
              value={search}
              onChange={e => setSearch(e.target.value)}
              onKeyDown={e => {
                if (e.key === 'Enter') {
                  setDebouncedSearch(search)
                }
              }}
              style={{ paddingLeft: '36px', paddingRight: search ? '36px' : '40px' }}
            />
            {search ? (
              <button
                type="button"
                onClick={() => setSearch('')}
                className="absolute right-2.5 p-1 rounded text-[var(--text-tertiary)] hover:text-[var(--text-primary)] cursor-pointer"
                aria-label="清空搜索"
              >
                <X className="w-3.5 h-3.5" strokeWidth={1.5} />
              </button>
            ) : (
              <kbd className="kbd hidden md:inline-flex absolute right-2.5 pointer-events-none">/</kbd>
            )}
          </div>

          {/* Account Chips */}
          {accounts.length > 0 && (
            <div className="flex items-center gap-1.5 overflow-x-auto pb-0.5 thin-scrollbar">
              <button
                type="button"
                onClick={() => setSelectedAccountId(null)}
                className={`inline-flex h-7 items-center gap-1.5 px-2.5 rounded-[6px] border text-xs whitespace-nowrap transition-colors cursor-pointer ${
                  selectedAccountId === null
                    ? 'bg-[var(--bg-tertiary)] text-[var(--text-primary)] font-medium border-[var(--border-default)]'
                    : 'bg-transparent text-[var(--text-secondary)] border-[var(--border-light)] hover:border-[var(--border-hover)]'
                }`}
              >
                <span>所有邮箱</span>
              </button>
              {accounts.map(account => {
                const providerText = providers.find(p => p.value === account.provider)?.label || account.provider
                const statusText = account.status === 'active' ? '已连接' : '未连接'
                const syncStatus = syncStatuses[String(account.id)]
                const isSyncing = syncStatus?.running === true
                const syncInfo = isSyncing ? '同步中' : formatSyncTime(account.last_sync_time)
                const tip = `${account.email} · ${providerText} · ${statusText} · ${syncInfo}`
                const isSelected = selectedAccountId === account.id
                const accountColor = account.color || '#2563EB'
                const shortName = account.email.split('@')[0]

                return (
                  <Tooltip key={account.id} content={tip}>
                    <button
                      type="button"
                      onClick={() => setSelectedAccountId(account.id)}
                      className={`inline-flex h-7 items-center gap-1.5 px-2.5 rounded-[6px] border text-xs whitespace-nowrap transition-colors cursor-pointer ${
                        isSelected
                          ? 'bg-[var(--bg-tertiary)] text-[var(--text-primary)] font-medium border-[var(--border-default)]'
                          : 'bg-transparent text-[var(--text-secondary)] border-[var(--border-light)] hover:border-[var(--border-hover)]'
                      }`}
                    >
                      {isSyncing ? (
                        <Loader2 className="w-3 h-3 animate-spin text-[var(--primary-500)]" strokeWidth={1.5} />
                      ) : (
                        <span
                          className="w-2 h-2 rounded-full flex-shrink-0"
                          style={{ backgroundColor: accountColor }}
                        />
                      )}
                      <span className="max-w-[110px] truncate">{shortName}</span>
                    </button>
                  </Tooltip>
                )
              })}
            </div>
          )}

          {/* Filter & Batch Row */}
          <div className="flex items-center justify-between gap-2">
            <div className="segmented">
              {(['all', 'unread'] as const).map((f) => (
                <button
                  key={f}
                  type="button"
                  onClick={() => setFilter(f)}
                  className={`segmented-item text-xs h-7 ${filter === f ? 'segmented-item-active' : ''}`}
                >
                  {f === 'all' ? '全部' : '未读'}
                </button>
              ))}
            </div>

            <button
              type="button"
              onClick={() => {
                setBatchMode(!batchMode)
                setSelectedIds(new Set())
              }}
              className={`h-7 px-2.5 rounded-[6px] text-xs border transition-colors cursor-pointer ${
                batchMode
                  ? 'bg-[var(--bg-tertiary)] text-[var(--text-primary)] font-medium border-[var(--border-default)]'
                  : 'text-[var(--text-secondary)] border-[var(--border-light)] hover:border-[var(--border-hover)] hover:text-[var(--text-primary)]'
              }`}
            >
              {batchMode ? '完成批量' : '批量'}
            </button>
          </div>

          {batchMode && (
            <div className="flex items-center justify-between gap-2 pt-1 text-xs flex-wrap">
              <div className="flex items-center gap-1.5">
                <button
                  type="button"
                  onClick={toggleSelectAll}
                  className="btn btn-secondary btn-sm h-7 px-2.5 text-xs"
                >
                  全选
                </button>
                <button
                  type="button"
                  onClick={toggleInvert}
                  className="btn btn-secondary btn-sm h-7 px-2.5 text-xs"
                >
                  反选
                </button>
              </div>
              {selectedIds.size > 0 ? (
                <div className="flex items-center gap-1.5">
                  <button
                    type="button"
                    onClick={handleBatchMarkAsRead}
                    className="btn btn-primary btn-sm h-7 px-2.5 text-xs"
                  >
                    <Check className="w-3.5 h-3.5" strokeWidth={1.5} />
                    <span>已读 ({selectedIds.size})</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setConfirmBatchDelete(true)}
                    className="btn btn-danger-ghost btn-sm h-7 px-2 text-xs"
                    title="删除选中邮件"
                    aria-label="删除选中邮件"
                  >
                    <Trash2 className="w-3.5 h-3.5" strokeWidth={1.5} />
                  </button>
                </div>
              ) : (
                <span className="font-mono tabular-nums text-xs text-[var(--text-tertiary)]">
                  已选 0 封
                </span>
              )}
            </div>
          )}
        </div>

        {/* Email List */}
        <div ref={listScrollRef} className="flex-1 overflow-auto thin-scrollbar" onScroll={handleScroll}>
          {loadingMore && emails.length === 0 ? (
            <div className="empty-state">
              <p className="text-xs text-[var(--text-tertiary)]">正在加载邮件</p>
            </div>
          ) : filteredEmails.length === 0 ? (
            <div className="empty-state">
              <Mail className="empty-state-icon" />
              <p className="text-sm font-medium text-[var(--text-primary)] mb-1">暂无邮件</p>
              <p className="text-xs text-[var(--text-tertiary)] mb-4">
                {search
                  ? '没有匹配当前关键词的邮件'
                  : filter === 'unread'
                  ? '当前筛选下没有未读邮件'
                  : '添加邮箱账户并同步后显示'}
              </p>
              {(search || filter === 'unread') && (
                <button
                  type="button"
                  onClick={() => {
                    setSearch('')
                    setFilter('all')
                  }}
                  className="btn btn-secondary btn-sm"
                >
                  显示全部邮件
                </button>
              )}
            </div>
          ) : (
            <div
              className="p-2"
              style={{
                paddingTop: `${virtualRange.paddingTop + 8}px`,
                paddingBottom: `${virtualRange.paddingBottom + 8}px`,
              }}
            >
              {visibleEmails.map(email => (
                <EmailListRow
                  key={email.id}
                  email={email}
                  accountColor={accountById.get(email.account_id)?.color || '#2563EB'}
                  dateLabel={emailDateMeta.get(email.id)?.label ?? '未知'}
                  isSelected={selectedEmail?.id === email.id}
                  batchMode={batchMode}
                  isChecked={selectedIds.has(email.id)}
                  registerRef={registerEmailRef}
                  onSelect={handleRead}
                  onContextMenu={handleRowContextMenu}
                  onToggleSelect={toggleSelect}
                  onToggleRead={handleToggleReadRow}
                  onRequestDelete={handleRequestDeleteRow}
                />
              ))}

              {loadingMore && (
                <div className="flex items-center justify-center py-4 gap-2">
                  <Loader2 className="w-4 h-4 text-[var(--text-tertiary)] animate-spin" strokeWidth={1.5} />
                  <span className="text-xs text-[var(--text-tertiary)]">加载中</span>
                </div>
              )}
              {!hasMore && emails.length > 0 && (
                <div className="text-center py-4 font-mono tabular-nums text-xs text-[var(--text-tertiary)]">
                  共 {total} 封邮件
                </div>
              )}
            </div>
          )}
        </div>

        {/* Keyboard Hints Footer */}
        <button
          type="button"
          onClick={() => setShowShortcuts(true)}
          className="hidden md:flex h-12 w-full px-4 border-t border-[var(--border-light)] bg-[var(--bg-sidebar)] items-center justify-between text-xs text-[var(--text-tertiary)] hover:text-[var(--text-secondary)] transition-colors flex-shrink-0 cursor-pointer"
          title="查看全部快捷键 (?)"
        >
          <div className="flex items-center gap-3">
            <span className="inline-flex items-center gap-1"><kbd className="kbd">j</kbd><kbd className="kbd">k</kbd> 移动</span>
            <span className="inline-flex items-center gap-1"><kbd className="kbd">u</kbd> 未读</span>
            <span className="inline-flex items-center gap-1"><kbd className="kbd">/</kbd> 搜索</span>
          </div>
          <span className="inline-flex items-center gap-1"><kbd className="kbd">?</kbd> 快捷键</span>
        </button>
      </div>

      {/* Right Column - Reading Canvas */}
      <div className={`${selectedEmail ? 'flex' : 'hidden md:flex'} flex-1 bg-[var(--bg-secondary)] min-h-0 flex-col`}>
        {selectedEmail ? (
          <div className="h-full flex flex-col">
            {/* Action Toolbar */}
            <div className="h-14 px-4 md:px-6 border-b border-[var(--border-light)] bg-[var(--bg-primary)] flex items-center justify-between gap-3 flex-shrink-0">
              <div className="flex items-center gap-2 min-w-0">
                <button
                  type="button"
                  onClick={() => setSelectedEmail(null)}
                  className="md:hidden btn btn-ghost btn-sm px-2"
                >
                  <ArrowLeft className="w-4 h-4" strokeWidth={1.5} />
                  <span>返回</span>
                </button>

                <div className="hidden sm:flex items-center gap-1 pr-2 border-r border-[var(--border-light)]">
                  <Tooltip content="上一封 (k)">
                    <button
                      type="button"
                      onClick={() => stepEmail(-1)}
                      disabled={selectedIndex <= 0}
                      className="btn btn-ghost w-7 h-7 p-0"
                      aria-label="上一封"
                    >
                      <ChevronUp className="w-4 h-4" strokeWidth={1.5} />
                    </button>
                  </Tooltip>
                  <Tooltip content="下一封 (j)">
                    <button
                      type="button"
                      onClick={() => stepEmail(1)}
                      disabled={selectedIndex < 0 || selectedIndex >= sortedFilteredEmails.length - 1}
                      className="btn btn-ghost w-7 h-7 p-0"
                      aria-label="下一封"
                    >
                      <ChevronDown className="w-4 h-4" strokeWidth={1.5} />
                    </button>
                  </Tooltip>
                  {selectedIndex >= 0 && (
                    <span className="font-mono tabular-nums text-xs text-[var(--text-tertiary)] pl-1">
                      {selectedIndex + 1}/{sortedFilteredEmails.length}
                    </span>
                  )}
                </div>

                {selectedEmailAccount && (
                  <div className="hidden lg:flex items-center gap-2 text-xs text-[var(--text-secondary)] min-w-0">
                    <span
                      className="w-2 h-2 rounded-full flex-shrink-0"
                      style={{ backgroundColor: selectedEmailAccount.color || '#2563EB' }}
                    />
                    <span className="truncate font-mono">{selectedEmailAccount.email}</span>
                  </div>
                )}
                {selectedEmail.folder && (
                  <span className="hidden xl:inline-flex px-2 py-0.5 rounded-[4px] bg-[var(--bg-tertiary)] border border-[var(--border-light)] font-mono text-xs text-[var(--text-tertiary)]">
                    {selectedEmail.folder}
                  </span>
                )}
              </div>

              <div className="flex items-center gap-2 flex-shrink-0">
                {resolvedTheme === 'dark' && selectedEmail.body_html && (
                  <Tooltip content={paperLight ? '切换为深色阅读背景' : '切换为白底信笺模式'}>
                    <button
                      type="button"
                      onClick={() => setPaperLight(prev => !prev)}
                      className="btn btn-secondary btn-sm px-2.5"
                    >
                      {paperLight ? (
                        <Moon className="w-3.5 h-3.5" strokeWidth={1.5} />
                      ) : (
                        <Sun className="w-3.5 h-3.5" strokeWidth={1.5} />
                      )}
                      <span className="hidden xl:inline">{paperLight ? '深色正文' : '白底信笺'}</span>
                    </button>
                  </Tooltip>
                )}
                <button
                  type="button"
                  onClick={() => handleMarkAsUnread(selectedEmail)}
                  disabled={!selectedEmail.is_read}
                  className="btn btn-secondary btn-sm"
                  title="标为未读 (u)"
                  aria-label="标为未读"
                >
                  <EyeOff className="w-4 h-4" strokeWidth={1.5} />
                  <span className="hidden sm:inline">标为未读</span>
                  <kbd className="kbd hidden lg:inline-flex">u</kbd>
                </button>
                <button
                  type="button"
                  onClick={() => handleSendToTelegram(selectedEmail)}
                  disabled={sendingTelegram}
                  className="btn btn-secondary btn-sm"
                  title="转发到 Telegram (t)"
                  aria-label="转发到 Telegram"
                >
                  {sendingTelegram ? (
                    <Loader2 className="w-4 h-4 animate-spin" strokeWidth={1.5} />
                  ) : (
                    <Send className="w-4 h-4" strokeWidth={1.5} />
                  )}
                  <span className="hidden sm:inline">转发 TG</span>
                </button>
                <button
                  type="button"
                  onClick={() => setConfirmDelete({ email: selectedEmail })}
                  className="btn btn-danger-ghost btn-sm"
                  title="删除邮件"
                  aria-label="删除邮件"
                >
                  <Trash2 className="w-4 h-4" strokeWidth={1.5} />
                  <span className="hidden sm:inline">删除</span>
                </button>
              </div>
            </div>

            {/* Mail Body Canvas */}
            <div ref={detailScrollRef} className="flex-1 overflow-auto thin-scrollbar p-4 md:p-8">
              <article className="max-w-3xl mx-auto rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] shadow-[var(--shadow-sm)] overflow-hidden">
                <header className="p-5 md:p-8 pb-6 border-b border-[var(--border-light)]">
                  <h1 className="text-xl md:text-2xl font-medium tracking-[-0.015em] text-[var(--text-primary)] break-words leading-snug">
                    {selectedEmail.subject || '(无主题)'}
                  </h1>

                  <div className="mt-5 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                    <div className="flex items-center gap-3 min-w-0">
                      <div className="avatar">
                        {(selectedEmail.from_name || selectedEmail.from)?.[0]?.toUpperCase() || '?'}
                      </div>
                      <div className="min-w-0">
                        <div className="flex items-center gap-2 flex-wrap">
                          <span className="text-sm font-medium text-[var(--text-primary)]">
                            {selectedEmail.from_name || selectedEmail.from}
                          </span>
                          <button
                            type="button"
                            onClick={() => {
                              navigator.clipboard?.writeText(selectedEmail.from).then(() => {
                                showToast('已复制发件人地址', 'success')
                              }).catch(() => {})
                            }}
                            className="inline-flex items-center gap-1.5 px-1.5 py-0.5 -mx-1.5 rounded-[4px] font-mono text-xs text-[var(--text-tertiary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-tertiary)] transition-colors cursor-pointer"
                            title="点击复制发件人邮箱"
                          >
                            <span className="truncate">{selectedEmail.from}</span>
                            <Copy className="w-3 h-3 opacity-60" strokeWidth={1.5} />
                          </button>
                        </div>
                        {selectedEmail.to && (
                          <div className="text-xs text-[var(--text-tertiary)] truncate mt-0.5">
                            收件人：<span className="font-mono">{selectedEmail.to}</span>
                          </div>
                        )}
                      </div>
                    </div>

                    <div className="flex items-center gap-2 flex-shrink-0">
                      {selectedEmail.has_attachment && (
                        <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-[4px] bg-[var(--bg-tertiary)] border border-[var(--border-light)] text-xs text-[var(--text-secondary)]">
                          <Paperclip className="w-3 h-3" strokeWidth={1.5} />
                          <span>附件</span>
                        </span>
                      )}
                      <time className="font-mono tabular-nums text-xs text-[var(--text-tertiary)]">
                        {new Date(selectedEmail.date).toLocaleString('zh-CN')}
                      </time>
                    </div>
                  </div>
                </header>

                <div
                  className={
                    useLightPaper && resolvedTheme === 'dark'
                      ? 'email-paper m-4 md:m-6 rounded-[8px] border border-[var(--border-default)] bg-[#FAFAFA] p-5 md:p-6'
                      : 'p-5 md:p-8'
                  }
                >
                  {loadingDetail ? (
                    <div className="flex items-center justify-center py-12 gap-2 text-sm text-[var(--text-tertiary)]">
                      <Loader2 className="w-4 h-4 animate-spin" strokeWidth={1.5} />
                      <span>正在加载正文</span>
                    </div>
                  ) : selectedEmail.body_html ? (
                    <div
                      className={`email-content overflow-x-auto ${
                        useLightPaper && resolvedTheme === 'dark' ? 'text-[#171717]' : ''
                      }`}
                      dangerouslySetInnerHTML={{ __html: sanitizedHtml }}
                    />
                  ) : selectedEmail.body_text ? (
                    <pre className="whitespace-pre-wrap break-words text-sm leading-relaxed text-[var(--text-primary)] font-sans">
                      {selectedEmail.body_text}
                    </pre>
                  ) : (
                    <p className="text-sm text-[var(--text-tertiary)]">无邮件内容</p>
                  )}
                </div>
              </article>
            </div>
          </div>
        ) : (
          <div className="h-full flex items-center justify-center p-8 text-[var(--text-tertiary)]">
            <div className="text-center max-w-xs">
              <Mail className="w-10 h-10 mx-auto mb-3 opacity-35" strokeWidth={1.5} />
              <p className="text-sm font-medium text-[var(--text-secondary)]">选择一封邮件阅读</p>
              <p className="mt-1 text-xs text-[var(--text-tertiary)]">
                使用 <kbd className="kbd">j</kbd> <kbd className="kbd">k</kbd> 移动，或按 <kbd className="kbd">?</kbd> 查看快捷键
              </p>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
