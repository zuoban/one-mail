import { useState, useEffect, useCallback, useRef, useMemo, memo } from 'react'
import { accountApi, telegramApi } from '../api'
import axios from 'axios'
import Tooltip from '../components/Tooltip'
import useTheme from '../context/useTheme'
import type { EmailSummary, EmailAccount } from '../api'
import useMail from '../mail/useMail'
import EmailBody from '../components/EmailBody'
import {
  Search,
  Mail,
  Paperclip,
  Loader2,
  Send,
  ArrowLeft,
  RefreshCw,
  X,
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

const EMAIL_ROW_HEIGHT = 72
const EMAIL_ROW_GAP = 4
const EMAIL_ROW_STRIDE = EMAIL_ROW_HEIGHT + EMAIL_ROW_GAP
const VIRTUAL_OVERSCAN = 8

interface EmailListRowProps {
  email: EmailSummary
  accountColor: string
  dateLabel: string
  isSelected: boolean
  registerRef: (id: number, node: HTMLDivElement | null) => void
  onSelect: (email: EmailSummary) => void
}

const EmailListRow = memo(function EmailListRow({
  email,
  accountColor,
  dateLabel,
  isSelected,
  registerRef,
  onSelect,
}: EmailListRowProps) {
  return (
    <div
      ref={(node) => registerRef(email.id, node)}
      role="group"
      tabIndex={0}
      aria-label={`${email.from_name || email.from}，${email.subject || '无主题'}`}
      aria-current={isSelected ? 'true' : undefined}
      onKeyDown={event => {
        if (event.target !== event.currentTarget) return
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          onSelect(email)
        }
      }}
      onClick={() => onSelect(email)}
      style={{ height: EMAIL_ROW_HEIGHT, marginBottom: EMAIL_ROW_GAP }}
      className={`mail-row group px-3 py-3 rounded-[8px] border cursor-pointer transition-colors ${
        isSelected
          ? 'bg-[var(--bg-accent-soft)] border-[var(--primary-200)]'
          : 'border-transparent hover:bg-[var(--bg-nav-hover)]'
      }`}
    >
      <div className="flex items-start gap-2">
        <div className="flex-1 min-w-0">
          <div className="flex h-5 items-center justify-between gap-2">
            <div className="flex items-center gap-2 min-w-0">
              <span
                className="w-1 h-3 rounded-full flex-shrink-0"
                style={{ backgroundColor: accountColor }}
              />
              <span
                className="text-[13px] truncate text-[var(--text-secondary)]"
              >
                {email.from_name || email.from}
              </span>
            </div>

            <div className="relative flex items-center justify-end flex-shrink-0 min-w-12">
              <span className="font-mono tabular-nums text-xs text-[var(--text-tertiary)] whitespace-nowrap">
                {dateLabel}
              </span>
            </div>
          </div>

          <div className="mt-1 flex items-center gap-2">
            <p
              className="text-sm truncate flex-1 text-[var(--text-primary)]"
            >
              {email.subject || '(无主题)'}
            </p>
            {email.has_attachment && (
              <Paperclip className="w-4 h-4 text-[var(--text-tertiary)] flex-shrink-0" strokeWidth={1.5} />
            )}
          </div>
        </div>
      </div>
    </div>
  )
})

export default function Dashboard() {
  const { resolvedTheme } = useTheme()
  const [accounts, setAccounts] = useState<EmailAccount[]>([])
  const [loadingAccounts, setLoadingAccounts] = useState(true)
  const [accountsError, setAccountsError] = useState('')
  const accountsRequest = useRef<AbortController | null>(null)
  const mounted = useRef(false)
  const lifecycle = useRef(0)
  const refreshPending = useRef(false)
  const [selectedAccountId, setSelectedAccountId] = useState<number | null>(null)
  const [search, setSearch] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [refreshing, setRefreshing] = useState(false)
  const { emails, selectedEmail, selectedDetail, loadingList, listError, loadingDetail, detailError, notice: toast, store } = useMail({
    search: debouncedSearch || undefined,
    account_id: selectedAccountId || undefined,
  })
  const showToast = store.notify
  const emailItemRefs = useRef<Record<number, HTMLDivElement | null>>({})
  const listScrollRef = useRef<HTMLDivElement | null>(null)
  const detailScrollRef = useRef<HTMLDivElement | null>(null)
  const searchInputRef = useRef<HTMLInputElement | null>(null)
  const listViewportHeightRef = useRef(600)
  const listScrollTopRef = useRef(0)
  const [virtualWindow, setVirtualWindow] = useState<{ start: number; end: number }>({ start: 0, end: 30 })
  const [sendingTelegram, setSendingTelegram] = useState(false)
  const [paperLight, setPaperLight] = useState(true)
  const [showShortcuts, setShowShortcuts] = useState(false)

  const formatSyncTime = (value?: string) => {
    if (!value) return '未刷新'
    const date = new Date(value)
    if (Number.isNaN(date.getTime()) || date.getFullYear() < 1970) return '未刷新'
    const diffMinutes = Math.floor((Date.now() - date.getTime()) / 60000)
    if (diffMinutes < 1) return '刚刚刷新'
    if (diffMinutes < 60) return `${diffMinutes} 分钟前刷新`
    const diffHours = Math.floor(diffMinutes / 60)
    if (diffHours < 24) return `${diffHours} 小时前刷新`
    const diffDays = Math.floor(diffHours / 24)
    if (diffDays < 7) return `${diffDays} 天前刷新`
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

  const invalidateLifecycle = useCallback(() => {
    mounted.current = false
    lifecycle.current++
    accountsRequest.current?.abort()
  }, [])

  const loadAccounts = useCallback(async (): Promise<boolean> => {
    if (!mounted.current) return false
    accountsRequest.current?.abort()
    const controller = new AbortController()
    accountsRequest.current = controller
    setLoadingAccounts(true)
    setAccountsError('')
    try {
      const res = await accountApi.list(controller.signal)
      if (!mounted.current || controller.signal.aborted || accountsRequest.current !== controller) return false
      setAccounts(res.data)
      return true
    } catch {
      if (mounted.current && !controller.signal.aborted && accountsRequest.current === controller) setAccountsError('账户列表加载失败，请重试')
      return false
    } finally {
      if (mounted.current && accountsRequest.current === controller) {
        accountsRequest.current = null
        setLoadingAccounts(false)
      }
    }
  }, [])


  useEffect(() => {
    const timer = window.setTimeout(() => {
      setDebouncedSearch(search)
    }, 300)
    return () => window.clearTimeout(timer)
  }, [search])

  const loadEmails = store.reload

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
    listScrollTopRef.current = target.scrollTop
    syncVirtualWindow(emails.length)
  }, [emails.length, syncVirtualWindow])

  useEffect(() => {
    mounted.current = true
    lifecycle.current++
    void loadAccounts()
    return invalidateLifecycle
  }, [loadAccounts, invalidateLifecycle])

  useEffect(() => {
    if (listScrollRef.current) {
      listScrollRef.current.scrollTop = 0
    }
    listScrollTopRef.current = 0
    syncVirtualWindow(0)
  }, [debouncedSearch, selectedAccountId, syncVirtualWindow])

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

  const resolveEmailDate = useCallback((email: EmailSummary) => {
    const primary = email.date || email.created_at
    const fallback = email.created_at || email.date
    const primaryDate = parseEmailDate(primary)
    if (primaryDate) return primaryDate
    return parseEmailDate(fallback)
  }, [parseEmailDate])

  const handleRefresh = useCallback(async () => {
    if (refreshPending.current || accounts.length === 0) return
    const generation = lifecycle.current
    refreshPending.current = true
    setRefreshing(true)
    try {
      const res = await store.refresh(selectedAccountId || undefined)
      if (!mounted.current || lifecycle.current !== generation) return
      const accountsUpdated = await loadAccounts()
      if (!mounted.current || lifecycle.current !== generation) return
      const errorEntries = res.errors ? Object.entries(res.errors) : []
      const messages: string[] = []
      if (errorEntries.length > 0) {
        const [failedAccount, failedReason] = errorEntries[0]
        messages.push(errorEntries.length === 1
          ? `${failedAccount} 刷新失败: ${failedReason}`
          : `刷新完成（${errorEntries.length} 个账户失败，如 ${failedAccount}）`)
      }
      if (!res.listUpdated) messages.push('邮箱同步请求已完成，但邮件列表未能更新，请重试读取邮件列表')
      if (!accountsUpdated) messages.push('账户列表未能更新，请重试读取账户')
      if (messages.length > 0) showToast(messages.join('；'), 'error')
      else showToast(res.new_count > 0 ? `已刷新，新增 ${res.new_count} 封邮件` : '已是最新邮件', 'success')
    } catch (e) {
      if (!mounted.current || lifecycle.current !== generation) return
      const message = axios.isAxiosError(e) ? e.response?.data?.error : undefined
      showToast(message || '刷新失败', 'error')
    } finally {
      if (mounted.current && lifecycle.current === generation) {
        refreshPending.current = false
        setRefreshing(false)
      }
    }
  }, [accounts.length, selectedAccountId, showToast, store, loadAccounts])

  const handleRead = useCallback(async (email: EmailSummary, shouldScroll = false, scrollDetail = false) => {
    if (scrollDetail) detailScrollRef.current?.scrollTo({ top: 0, behavior: 'smooth' })
    if (shouldScroll) emailItemRefs.current[email.id]?.scrollIntoView({ block: 'nearest' })
    await store.open(email)
  }, [store])

  const handleSendToTelegram = useCallback(async (email: EmailSummary) => {
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

  const filteredEmails = emails


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

  const accountById = useMemo(() => {
    const map = new Map<number, EmailAccount>()
    accounts.forEach(a => map.set(a.id, a))
    return map
  }, [accounts])

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
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
      } else if (event.key === 't' || event.key === 'T') {
        if (selectedEmail && !sendingTelegram) {
          event.preventDefault()
          handleSendToTelegram(selectedEmail)
        }
      } else if (event.key === 'r' || event.key === 'R') {
        event.preventDefault()
        handleRefresh()
      } else if (event.key === 'Escape' && selectedEmail) {
        store.closeDetail()
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [
    handleRefresh,
    handleRead,
    scrollToEmailIndex,
    handleSendToTelegram,
    sendingTelegram,
    selectedEmail,
    sortedFilteredEmails,
    showShortcuts,
    showToast,
    store,
  ])


  const selectedEmailAccount = selectedEmail ? accountById.get(selectedEmail.account_id) : undefined
  const useLightPaper = Boolean(selectedDetail?.body_html) && (resolvedTheme === 'light' || paperLight)

  return (
    <div className={`mail-workspace h-full flex bg-[var(--bg-secondary)] overflow-hidden ${selectedEmail ? 'is-reading' : ''}`}>
      {/* Toast */}
      {toast && (
        <div className="fixed left-4 right-4 top-4 z-50 md:left-auto md:right-4 animate-in fade-in slide-in-from-top-2 duration-200">
          <div
            role={toast.type === 'error' ? 'alert' : 'status'}
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
          </div>
        </div>
      )}

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
                { keys: ['t'], desc: '转发到 Telegram' },
                { keys: ['r'], desc: '刷新邮件列表' },
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


      {/* Left Column - Mail List */}
      <section aria-label="邮件列表" className="mail-list-pane min-w-0 border-r border-[var(--border-light)] bg-[var(--bg-primary)] flex-col min-h-0">
        {/* Inbox header */}
        <div data-refreshing={refreshing} className="mail-list-header h-16 px-4 border-b border-[var(--border-light)] flex items-center justify-between gap-2 flex-shrink-0">
          <div className="flex flex-col gap-1 min-w-0">
            <h2 className="text-lg font-medium text-[var(--text-primary)]">收件箱</h2>
            <span id="inbox-refresh-status" role="status" className={`text-xs tabular-nums ${refreshing ? 'text-[var(--primary-600)]' : 'font-mono text-[var(--text-tertiary)]'}`}>
              {refreshing ? '正在获取最近 50 封邮件' : `${filteredEmails.length} 封`}
            </span>
          </div>
          <Tooltip content={refreshing ? '正在连接邮箱，已有邮件可继续阅读' : '拉取最近 50 封邮件 (r)'}>
            <button
              type="button"
              onClick={handleRefresh}
              disabled={refreshing || accounts.length === 0}
              aria-busy={refreshing}
              aria-describedby={refreshing ? 'inbox-refresh-status' : undefined}
              data-refreshing={refreshing}
              className="refresh-button btn btn-ghost px-3"
            >
              {refreshing ? <Loader2 aria-hidden="true" className="w-4 h-4 animate-spin" strokeWidth={1.5} /> : <RefreshCw aria-hidden="true" className="w-4 h-4" strokeWidth={1.5} />}
              <span>{refreshing ? '刷新中' : '刷新'}</span>
            </button>
          </Tooltip>
        </div>

        {/* Search & Filter Controls */}
        <div className="mail-list-controls px-4 py-4 border-b border-[var(--border-light)] space-y-3 flex-shrink-0">
          {/* Search */}
          <div className="relative flex items-center">
            <Search className="absolute left-3 w-4 h-4 text-[var(--text-tertiary)] pointer-events-none" strokeWidth={1.5} />
            <input
              ref={searchInputRef}
              type="text"
              placeholder="搜索发件人或主题"
              aria-label="搜索发件人或主题"
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
                className="absolute right-2 p-1 rounded text-[var(--text-tertiary)] hover:text-[var(--text-primary)] cursor-pointer"
                aria-label="清空搜索"
              >
                <X className="w-3.5 h-3.5" strokeWidth={1.5} />
              </button>
            ) : (
              <kbd className="kbd hidden md:inline-flex absolute right-2 pointer-events-none">/</kbd>
            )}
          </div>

          {loadingAccounts && <p role="status" className="text-xs text-[var(--text-secondary)]">正在加载账户列表…</p>}
          {accountsError && <div role="alert" className="text-sm text-[var(--error-700)]">{accountsError}<button type="button" disabled={loadingAccounts} onClick={() => void loadAccounts()} className="btn btn-secondary btn-sm ml-2">重试读取账户</button></div>}
          {/* Account Chips */}
          {accounts.length > 0 && (
            <div className="account-filters flex items-center gap-2 overflow-x-auto pb-1 thin-scrollbar" aria-label="按邮箱筛选">
              <button
                type="button"
                onClick={() => setSelectedAccountId(null)}
                aria-pressed={selectedAccountId === null}
                className={`inline-flex h-8 items-center gap-2 px-3 rounded-[8px] border text-xs whitespace-nowrap transition-colors cursor-pointer ${
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
                const syncInfo = formatSyncTime(account.last_sync_time)
                const tip = `${account.email} · ${providerText} · ${statusText} · ${syncInfo}`
                const isSelected = selectedAccountId === account.id
                const accountColor = account.color || '#2563EB'
                const shortName = account.email.split('@')[0]

                return (
                  <Tooltip key={account.id} content={tip}>
                    <button
                      type="button"
                      onClick={() => setSelectedAccountId(account.id)}
                      aria-pressed={isSelected}
                      className={`inline-flex h-8 items-center gap-2 px-3 rounded-[8px] border text-xs whitespace-nowrap transition-colors cursor-pointer ${
                        isSelected
                          ? 'bg-[var(--bg-tertiary)] text-[var(--text-primary)] font-medium border-[var(--border-default)]'
                          : 'bg-transparent text-[var(--text-secondary)] border-[var(--border-light)] hover:border-[var(--border-hover)]'
                      }`}
                    >
                      <span
                        className="w-2 h-2 rounded-full flex-shrink-0"
                        style={{ backgroundColor: accountColor }}
                      />
                      <span className="max-w-[144px] truncate">{shortName}</span>
                    </button>
                  </Tooltip>
                )
              })}
            </div>
          )}

        </div>

        {/* Email List */}
        <div ref={listScrollRef} className="flex-1 overflow-auto thin-scrollbar" onScroll={handleScroll}>
          {listError && <div role="alert" className="p-4 text-sm text-[var(--error-700)]">{listError}<button type="button" disabled={loadingList} onClick={() => void loadEmails()} className="btn btn-secondary btn-sm ml-2">重试</button></div>}
          {(loadingList || refreshing) && emails.length === 0 ? (
            <div className="empty-state h-full min-h-48 px-6">
              <div className="refresh-empty-icon" aria-hidden="true">
                <Loader2 className="w-6 h-6 animate-spin" strokeWidth={1.5} />
              </div>
              <p className="text-sm font-medium text-[var(--text-primary)]">{refreshing ? '正在刷新邮件' : '正在加载邮件'}</p>
              <p className="mt-2 text-xs leading-5 text-[var(--text-secondary)]">{refreshing ? '连接邮箱并获取最近 50 封邮件' : '正在读取邮件列表'}</p>
            </div>
          ) : filteredEmails.length === 0 && !listError ? (
            <div className="empty-state">
              <Mail className="empty-state-icon" />
              <p className="text-sm font-medium text-[var(--text-primary)] mb-1">暂无邮件</p>
              <p className="text-xs text-[var(--text-tertiary)] mb-4">
                {search
                  ? '没有匹配当前关键词的邮件'
                  : '添加邮箱账户并点击刷新获取最近 50 封邮件'}
              </p>
              {search && (
                <button
                  type="button"
                  onClick={() => {
                    setSearch('')
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
                  registerRef={registerEmailRef}
                  onSelect={handleRead}
                />
              ))}

              {emails.length > 0 && (
                <div className="text-center py-4 font-mono tabular-nums text-xs text-[var(--text-tertiary)]">
                  仅展示最近 50 封邮件 · 当前 {emails.length} 封
                </div>
              )}
            </div>
          )}
        </div>

        {/* Keyboard Hints Footer */}
        <button
          type="button"
          onClick={() => setShowShortcuts(true)}
          className="mail-keyboard-hints hidden lg:flex h-14 w-full px-4 border-t border-[var(--border-light)] bg-[var(--bg-primary)] items-center justify-between gap-2 text-xs text-[var(--text-tertiary)] hover:text-[var(--text-secondary)] transition-colors flex-shrink-0 cursor-pointer"
          title="查看全部快捷键 (?)"
        >
          <div className="flex items-center gap-2">
            <span className="inline-flex items-center gap-1"><kbd className="kbd">j</kbd><kbd className="kbd">k</kbd> 移动</span>
            <span className="inline-flex items-center gap-1"><kbd className="kbd">r</kbd> 刷新</span>
            <span className="inline-flex items-center gap-1"><kbd className="kbd">/</kbd> 搜索</span>
          </div>
          <span className="inline-flex items-center gap-1"><kbd className="kbd">?</kbd> 快捷键</span>
        </button>
      </section>

      {/* Right Column - Reading Canvas */}
      <section aria-label="邮件阅读区" className="mail-reader flex-1 min-w-0 bg-[var(--bg-secondary)] min-h-0 flex-col">
        {selectedEmail ? (
          <div className="h-full flex flex-col">
            {/* Action Toolbar */}
            <div className="reading-toolbar h-16 px-4 xl:px-6 border-b border-[var(--border-light)] bg-[var(--bg-primary)] flex items-center justify-between gap-2 flex-shrink-0">
              <div className="flex items-center gap-2 min-w-0">
                <button
                  type="button"
                  onClick={store.closeDetail}
                  className="lg:hidden btn btn-ghost btn-sm px-2"
                >
                  <ArrowLeft className="w-4 h-4" strokeWidth={1.5} />
                  <span>返回</span>
                </button>

                <div className="hidden lg:flex items-center gap-1 pr-2 border-r border-[var(--border-light)]">
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
                  <div className="hidden 2xl:flex items-center gap-2 text-xs text-[var(--text-secondary)] min-w-0">
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
                {resolvedTheme === 'dark' && selectedDetail?.body_html && (
                  <Tooltip content={paperLight ? '切换为深色阅读背景' : '切换为白底信笺模式'}>
                    <button
                      type="button"
                      onClick={() => setPaperLight(prev => !prev)}
                      className="btn btn-secondary btn-sm px-2"
                      aria-label={paperLight ? '切换为深色阅读背景' : '切换为白底信笺模式'}
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
                  <span className="hidden xl:inline">转发 Telegram</span>
                </button>
              </div>
            </div>

            {/* Mail Body Canvas */}
            <div ref={detailScrollRef} className="reading-canvas flex-1 overflow-auto thin-scrollbar p-4 xl:p-8">
              <article className="mail-letter max-w-3xl mx-auto rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] shadow-[var(--shadow-sm)] overflow-hidden">
                <header className="p-6 xl:p-8 border-b border-[var(--border-light)]">
                  <h1 className="text-xl xl:text-2xl font-medium text-[var(--text-primary)] break-words leading-snug">
                    {selectedEmail.subject || '(无主题)'}
                  </h1>

                  <div className="mt-6 flex flex-col gap-4 2xl:flex-row 2xl:items-center 2xl:justify-between">
                    <div className="flex items-start gap-3 min-w-0">
                      <div className="avatar">
                        {(selectedEmail.from_name || selectedEmail.from)?.[0]?.toUpperCase() || '?'}
                      </div>
                      <div className="min-w-0">
                        <div className="flex flex-col items-start gap-1">
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
                            className="sender-address inline-flex max-w-full items-center gap-2 px-1 py-1 -mx-1 rounded-[4px] font-mono text-xs text-[var(--text-tertiary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-tertiary)] transition-colors cursor-pointer"
                            title="点击复制发件人邮箱"
                          >
                            <span className="truncate">{selectedEmail.from}</span>
                            <Copy className="w-4 h-4 flex-shrink-0" strokeWidth={1.5} />
                          </button>
                        </div>
                        {selectedEmail.to && (
                          <div className="text-xs text-[var(--text-tertiary)] break-all mt-1">
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
                      ? 'email-paper m-4 xl:m-6 rounded-[8px] border border-[var(--border-default)] bg-[#FAFAFA] p-6'
                      : 'p-6 xl:p-8'
                  }
                >
                  {loadingDetail ? (
                    <div className="flex items-center justify-center py-12 gap-2 text-sm text-[var(--text-tertiary)]">
                      <Loader2 className="w-4 h-4 animate-spin" strokeWidth={1.5} />
                      <span>正在加载正文</span>
                    </div>
                  ) : detailError ? (
                    <div role="alert" className="text-sm text-[var(--error-700)]">
                      <p>{detailError}</p>
                      <button type="button" className="btn btn-secondary btn-sm mt-3" onClick={() => void store.retryDetail()}>重试加载正文</button>
                    </div>
                  ) : selectedDetail?.body_html ? (
                    <EmailBody key={selectedDetail.id} html={selectedDetail.body_html} light={useLightPaper} />
                  ) : selectedDetail?.body_text ? (
                    <pre className="whitespace-pre-wrap break-words text-sm leading-relaxed text-[var(--text-primary)] font-sans">
                      {selectedDetail.body_text}
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
              <Mail className="w-10 h-10 mx-auto mb-4 opacity-35" strokeWidth={1.5} />
              <p className="text-sm font-medium text-[var(--text-secondary)]">选择一封邮件阅读</p>
              <p className="mt-2 text-xs text-[var(--text-tertiary)] leading-relaxed">
                使用 <kbd className="kbd">j</kbd> <kbd className="kbd">k</kbd> 移动，或按 <kbd className="kbd">?</kbd> 查看快捷键
              </p>
            </div>
          </div>
        )}
      </section>
    </div>
  )
}
