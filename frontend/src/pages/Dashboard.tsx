import { useState, useEffect, useCallback, useRef, useMemo } from 'react'
import Tooltip from '../components/Tooltip'
import type { EmailSummary, EmailAccount } from '../api'
import useMail from '../mail/useMail'
import MailReader from '../mail/MailReader'
import MailList from '../mail/MailList'
import { useInboxActions } from '../mail/useInboxActions'
import { useInboxKeyboard } from '../mail/useInboxKeyboard'
import { useAccounts } from '../accounts/account-context'
import useReadingPreferences from '../context/useReadingPreferences'
import {
  Search,
  Loader2,
  RefreshCw,
  X,
  Keyboard,
  Inbox,
} from 'lucide-react'

export default function Dashboard() {
  const { accounts, loadingAccounts, accountsError, loadAccounts, providersError, loadingProviders, accountStore } = useAccounts()
  const { emailLimit } = useReadingPreferences()
  const [selectedAccountId, setSelectedAccountId] = useState<number | null>(null)
  const [search, setSearch] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const { emails, selectedEmail, selectedDetail, loadingList, listError, loadingDetail, detailError, notice: toast, store } = useMail({
    search: debouncedSearch || undefined,
    account_id: selectedAccountId || undefined,
    limit: emailLimit,
  })
  const showToast = store.notify
  const emailItemRefs = useRef<Record<number, HTMLDivElement | null>>({})
  const listScrollRef = useRef<HTMLDivElement | null>(null)
  const detailScrollRef = useRef<HTMLDivElement | null>(null)
  const searchInputRef = useRef<HTMLInputElement | null>(null)
  const { refreshing, sendingTelegram, handleRefresh, handleSendToTelegram } = useInboxActions(accounts, selectedAccountId, store, accountStore.invalidate)
  const [showShortcuts, setShowShortcuts] = useState(false)
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 60000)
    return () => window.clearInterval(timer)
  }, [])


  const formatRelativeTime = useCallback((date: Date | null) => {
    if (!date) return '未知'
    const currentDate = new Date(now)
    const diffMs = now - date.getTime()
    if (diffMs < 0) return date.toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' })

    const diffMinutes = Math.floor(diffMs / 60000)
    if (diffMinutes < 1) return '刚刚'
    if (diffMinutes < 60) return `${diffMinutes}分`

    const diffHours = Math.floor(diffMinutes / 60)
    if (diffHours < 24) return `${diffHours}时`

    const diffDays = Math.floor(diffHours / 24)
    if (diffDays === 1) return '昨天'
    if (diffDays < 7) return `${diffDays}天`

    if (date.getFullYear() === currentDate.getFullYear()) {
      return `${date.getMonth() + 1}/${date.getDate()}`
    }
    return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`
  }, [now])

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setDebouncedSearch(search)
    }, 300)
    return () => window.clearTimeout(timer)
  }, [search])

  const loadEmails = store.reload

  useEffect(() => {
    if (listScrollRef.current) listScrollRef.current.scrollTop = 0
  }, [debouncedSearch, selectedAccountId])

  const parseEmailDate = useCallback((value?: string | null) => {
    if (!value) return null
    const direct = new Date(value)
    if (!Number.isNaN(direct.getTime())) return direct
    return null
  }, [])

  const resolveEmailDate = useCallback((email: EmailSummary) => {
    const primary = email.date || email.created_at
    const fallback = email.created_at || email.date
    const primaryDate = parseEmailDate(primary)
    if (primaryDate) return primaryDate
    return parseEmailDate(fallback)
  }, [parseEmailDate])

  const handleRead = useCallback(async (email: EmailSummary, shouldScroll = false, scrollDetail = false) => {
    if (scrollDetail) detailScrollRef.current?.scrollTo({ top: 0, behavior: 'smooth' })
    if (shouldScroll) emailItemRefs.current[email.id]?.scrollIntoView({ block: 'nearest' })
    await store.open(email)
  }, [store])

  const filteredEmails = useMemo(() => emails.slice(0, emailLimit), [emails, emailLimit])


  // 预处理每封邮件的时间戳与相对时间字符串，避免排序和滚动渲染期间重复 Date 解析
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
  }, [filteredEmails, resolveEmailDate, formatRelativeTime])

  const sortedFilteredEmails = useMemo(() =>
    [...filteredEmails].sort((a, b) => {
      const tsA = emailDateMeta.get(a.id)?.ts ?? 0
      const tsB = emailDateMeta.get(b.id)?.ts ?? 0
      if (tsB !== tsA) return tsB - tsA
      return b.id - a.id
    }),
    [filteredEmails, emailDateMeta],
  )

  const registerEmailRef = useCallback((id: number, node: HTMLDivElement | null) => {
    if (node) {
      emailItemRefs.current[id] = node
    } else {
      delete emailItemRefs.current[id]
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
      handleRead(target, true, true)
    }
  }, [sortedFilteredEmails, selectedIndex, handleRead])

  const accountById = useMemo(() => {
    const map = new Map<number, EmailAccount>()
    accounts.forEach(a => map.set(a.id, a))
    return map
  }, [accounts])

  useInboxKeyboard({ searchInputRef, sortedFilteredEmails, selectedEmail, showShortcuts, setShowShortcuts, sendingTelegram, handleRead, handleSendToTelegram, handleRefresh, closeDetail: store.closeDetail })

  const selectedEmailAccount = selectedEmail ? accountById.get(selectedEmail.account_id) : undefined

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
              {refreshing ? `正在获取最近 ${emailLimit} 封邮件` : `${filteredEmails.length} 封`}
            </span>
          </div>
          <Tooltip content={refreshing ? '正在连接邮箱，已有邮件可继续阅读' : `拉取最近 ${emailLimit} 封邮件 (r)`}>
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
          {accounts.length > 0 && (
            <div className="relative flex items-center">
              <Inbox aria-hidden="true" className="absolute left-3 w-4 h-4 text-[var(--text-secondary)] pointer-events-none" strokeWidth={1.5} />
              <label htmlFor="inbox-account-filter" className="sr-only">按邮箱筛选</label>
              <select
                id="inbox-account-filter"
                value={selectedAccountId ?? ''}
                onChange={event => setSelectedAccountId(event.target.value === '' ? null : Number(event.target.value))}
                className="min-w-0 text-ellipsis"
                style={{ paddingLeft: '36px' }}
              >
                <option value="">所有邮箱 · {accounts.length} 个账户</option>
                {accounts.map(account => <option key={account.id} value={account.id}>{account.email}</option>)}
              </select>
            </div>
          )}

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
          {providersError && <div role="alert" className="text-sm text-[var(--error-700)]">{providersError}<button type="button" disabled={loadingProviders} onClick={() => void accountStore.retryProviders()} className="btn btn-secondary btn-sm ml-2">重试读取服务商</button></div>}

        </div>

        {/* Email List */}
        <MailList emails={sortedFilteredEmails} selectedId={selectedEmail?.id} accountById={accountById} dateLabels={emailDateMeta}
          showAccountEmail={selectedAccountId === null && accounts.length > 1}
          limit={emailLimit}
          loading={loadingList} refreshing={refreshing} error={listError} search={search} scrollRef={listScrollRef}
          retry={loadEmails} clearSearch={() => setSearch('')} registerRef={registerEmailRef} onSelect={handleRead} />

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
      <MailReader selectedEmail={selectedEmail} selectedDetail={selectedDetail} selectedEmailAccount={selectedEmailAccount}
        selectedIndex={selectedIndex} total={sortedFilteredEmails.length} loadingDetail={loadingDetail} detailError={detailError}
        sendingTelegram={sendingTelegram} detailScrollRef={detailScrollRef} stepEmail={stepEmail} closeDetail={store.closeDetail}
        retryDetail={store.retryDetail} handleSendToTelegram={handleSendToTelegram} showToast={showToast} />
    </div>
  )
}
