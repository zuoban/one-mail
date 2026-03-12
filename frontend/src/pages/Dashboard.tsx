import { useState, useEffect, useCallback, useRef, useMemo } from 'react'
import { emailApi, accountApi } from '../api'
import ConfirmDialog from '../components/ConfirmDialog'
import Tooltip from '../components/Tooltip'
import type { Email, EmailAccount } from '../api'
import { Search, RefreshCw, Mail, Paperclip, Trash2, Eye, EyeOff, Plug } from 'lucide-react'

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
  const [loading, setLoading] = useState(false)
  const [selectedEmail, setSelectedEmail] = useState<Email | null>(null)
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState<'all' | 'unread' | 'attachments'>('all')
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


  const loadAccounts = useCallback(async () => {
    try {
      const res = await accountApi.list()
      setAccounts(res.data)
    } catch (e) {
      console.error(e)
    }
  }, [])

  const loadEmails = useCallback(async () => {
    setLoading(true)
    try {
      const res = await emailApi.list({ 
        page: 1, 
        page_size: 50, 
        search,
        account_id: selectedAccountId || undefined,
      })
      setEmails(res.data)
    } catch (e) {
      console.error(e)
    } finally {
      setLoading(false)
    }
  }, [search, selectedAccountId])

  useEffect(() => {
    loadAccounts()
    loadEmails()
  }, [loadAccounts, loadEmails])

  const handleSync = async () => {
    let failed = 0
    for (const account of accounts) {
      try {
        await accountApi.sync(account.id)
      } catch (e) {
        console.error(e)
        failed += 1
      }
    }
    loadEmails()
    if (failed > 0) {
      showToast(`同步完成，失败 ${failed} 个账户`, 'error')
    } else {
      showToast('同步完成', 'success')
    }
  }

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

  const handleRead = useCallback(async (email: Email, shouldScroll = false, scrollDetail = false) => {
    if (!email.is_read) {
      await emailApi.markAsRead(email.id)
      setEmails(prev => prev.map(e => e.id === email.id ? { ...e, is_read: true } : e))
    }
    setSelectedEmail(email)
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

  const closeContextMenu = useCallback(() => {
    setContextMenu(null)
    setContextMenuIndex(0)
  }, [])

  const filteredEmails = emails.filter(email => {
    if (filter === 'unread') return !email.is_read
    if (filter === 'attachments') return email.has_attachment
    return true
  })

  const getGroupKey = (dateValue: string) => {
    const date = new Date(dateValue)
    const now = new Date()
    const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate())
    const dayOfWeek = (startOfToday.getDay() + 6) % 7
    const startOfWeek = new Date(startOfToday)
    startOfWeek.setDate(startOfToday.getDate() - dayOfWeek)

    if (date >= startOfToday) return 'today'
    if (date >= startOfWeek) return 'week'
    return 'earlier'
  }

  const sortedFilteredEmails = [...filteredEmails].sort((a, b) =>
    new Date(b.date).getTime() - new Date(a.date).getTime(),
  )

  const groupedEmails = sortedFilteredEmails.reduce<Record<string, Email[]>>((acc, email) => {
    const key = getGroupKey(email.date)
    if (!acc[key]) acc[key] = []
    acc[key].push(email)
    return acc
  }, {})

  const groupOrder: Array<{ key: string; label: string }> = [
    { key: 'today', label: '今天' },
    { key: 'week', label: '本周' },
    { key: 'earlier', label: '更早' },
  ]

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
        handleSync()
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [loadEmails, handleRead, selectedEmail, sortedFilteredEmails, contextMenu])

  const contextMenuItems = useMemo(() => {
    if (!contextMenu) return []
    const items = [
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
    <div className="h-full flex bg-slate-50 overflow-hidden">
      {toast && (
        <div className="fixed top-4 right-4 z-50">
          <div className={`flex items-center gap-3 px-4 py-2 rounded-lg text-sm shadow-lg border ${toast.type === 'success' ? 'bg-emerald-50 text-emerald-700 border-emerald-200' : 'bg-rose-50 text-rose-700 border-rose-200'}`}>
            <span>{toast.message}</span>
            {toast.actionLabel && toast.onAction && (
              <button
                type="button"
                onClick={() => {
                  const action = toast.onAction
                  setToast(null)
                  action()
                }}
                className="text-xs font-semibold text-slate-700 hover:text-slate-900"
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
      {contextMenu && (
        <div
          className="fixed z-50 w-48 rounded-lg border border-slate-200 bg-white shadow-xl py-1 text-sm text-slate-700"
          style={{ top: contextMenu.y, left: contextMenu.x }}
          onClick={closeContextMenu}
        >
          {contextMenuItems.map((item, index) => (
            <button
              key={`${item.label}-${index}`}
              type="button"
              onClick={item.action}
              className={`w-full text-left px-3 py-2 ${
                index === contextMenuIndex ? 'bg-slate-50' : 'hover:bg-slate-50'
              } ${item.danger ? 'text-rose-600 hover:bg-rose-50' : ''}`}
            >
              {item.label}
            </button>
          ))}
        </div>
      )}
      <div className="w-96 border-r border-slate-200/70 bg-white flex flex-col min-h-0">
        <div className="p-4 border-b border-slate-200/70 bg-gradient-to-b from-slate-50/80 to-white flex-shrink-0">
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2 text-slate-900">
              <Mail className="w-4 h-4 text-slate-500" />
              <span className="text-sm font-semibold">收件箱</span>
            </div>
            <span className="text-xs text-slate-500 bg-slate-100 px-2 py-0.5 rounded-full">
              {filteredEmails.length}
            </span>
          </div>
          {accounts.length > 0 && (
            <div className="mb-4">
              <div className="mb-2 text-xs text-slate-500">账户</div>
              {(() => {
                const renderAllButton = (isSelected: boolean) => (
                  <button
                    type="button"
                    onClick={() => setSelectedAccountId(null)}
                    className={`flex flex-col items-center justify-center gap-1 px-2 py-2 rounded-lg border text-[11px] font-medium whitespace-nowrap transition-all w-20 ${
                      isSelected
                        ? 'bg-blue-600 text-white border-blue-600 shadow-sm'
                        : 'bg-white text-slate-600 border-slate-200 hover:border-slate-300'
                    }`}
                  >
                    <span className={`flex items-center justify-center w-5 h-5 rounded-full text-[11px] font-semibold ${
                      isSelected ? 'bg-white/20 text-white' : 'bg-blue-50 text-blue-700'
                    }`}>
                      <Mail className="w-3.5 h-3.5" />
                    </span>
                    <span className={`leading-tight ${isSelected ? 'text-blue-100' : 'text-slate-600'}`}>
                      全部
                    </span>
                  </button>
                )

                const renderAccountButton = (account: EmailAccount, isSelected: boolean) => {
                  const providerText = providers.find(p => p.value === account.provider)?.label || account.provider
                  const statusText = account.status === 'active' ? '已连接' : '未连接'
                  const tip = `${account.email} · ${providerText} · ${statusText} · ${formatSyncTime(account.last_sync_time)}`
                  return (
                    <Tooltip content={tip}>
                      <button
                        key={account.id}
                        type="button"
                        onClick={() => setSelectedAccountId(account.id)}
                        className={`flex flex-col items-center justify-center gap-1 px-2 py-2 rounded-lg border text-[11px] font-medium whitespace-nowrap transition-all w-20 ${
                          isSelected
                            ? 'bg-blue-600 text-white border-blue-600 shadow-sm'
                            : 'bg-white text-slate-600 border-slate-200 hover:border-slate-300'
                        }`}
                      >
                        <span className={`flex items-center justify-center w-5 h-5 rounded-full text-[11px] font-semibold ${
                          isSelected ? 'bg-white/20 text-white' : 'bg-blue-50 text-blue-700'
                        }`}>
                          {account.email?.[0]?.toUpperCase() || '?'}
                        </span>
                        <span className={`leading-tight ${isSelected ? 'text-blue-100' : 'text-slate-600'}`}>
                          {providerText}
                        </span>
                      </button>
                    </Tooltip>
                  )
                }
                return (
                  <div className="flex flex-wrap gap-2 max-h-40 overflow-y-auto pr-1 thin-scrollbar">
                    {renderAllButton(selectedAccountId === null)}
                    {accounts.map(account => renderAccountButton(account, selectedAccountId === account.id))}
                  </div>
                )
              })()}
              <div className="mt-2 flex items-center gap-2 text-[11px] text-slate-400">
                <Plug className="w-3.5 h-3.5" />
                点击账户卡片切换，双向同步仍适用于全部账户
              </div>
            </div>
          )}
          <div className="flex items-center gap-2 mb-3">
            <button
              type="button"
              onClick={() => setFilter('all')}
              className={`px-3 py-1.5 text-xs rounded-full border transition-colors ${
                filter === 'all'
                  ? 'bg-slate-900 text-white border-slate-900'
                  : 'bg-white text-slate-600 border-slate-200 hover:border-slate-300'
              }`}
            >
              全部
            </button>
            <button
              type="button"
              onClick={() => setFilter('unread')}
              className={`px-3 py-1.5 text-xs rounded-full border transition-colors ${
                filter === 'unread'
                  ? 'bg-blue-600 text-white border-blue-600'
                  : 'bg-white text-slate-600 border-slate-200 hover:border-slate-300'
              }`}
            >
              未读
            </button>
            <button
              type="button"
              onClick={() => setFilter('attachments')}
              className={`px-3 py-1.5 text-xs rounded-full border transition-colors ${
                filter === 'attachments'
                  ? 'bg-emerald-600 text-white border-emerald-600'
                  : 'bg-white text-slate-600 border-slate-200 hover:border-slate-300'
              }`}
            >
              附件
            </button>
          </div>
          <div className="mb-3 text-[11px] text-slate-400">
            快捷键：j/k 上下，o 打开，r 刷新
          </div>
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400" />
            <input
              type="text"
              placeholder="搜索邮件..."
              value={search}
              onChange={e => setSearch(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && loadEmails()}
              className="w-full pl-10 pr-4 py-2.5 border border-slate-200/80 rounded-xl bg-white/90 shadow-sm focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-400"
            />
          </div>
          <button
            onClick={handleSync}
            disabled={loading}
            className="mt-3 w-full flex items-center justify-center gap-2 py-2.5 bg-blue-600 text-white rounded-xl hover:bg-blue-700 disabled:opacity-50 shadow-sm"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            同步邮件
          </button>
        </div>
        <div className="flex-1 overflow-auto bg-slate-50">
          {filteredEmails.length === 0 ? (
            <div className="p-8 text-center text-slate-500">
              <Mail className="w-12 h-12 mx-auto mb-3 opacity-30" />
              <p>暂无邮件</p>
              <p className="text-sm mt-1">请先添加邮箱账户</p>
            </div>
          ) : (
            <div className="pt-2 pb-4">
              {groupOrder.map(group => (
                groupedEmails[group.key]?.length ? (
                  <div key={group.key}>
                    <div className="px-4 py-2 text-xs font-semibold text-slate-500 uppercase tracking-wide">
                      {group.label}
                    </div>
                    {groupedEmails[group.key].map(email => (
                      <div
                        key={email.id}
                        ref={(node) => {
                          emailItemRefs.current[email.id] = node
                        }}
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
                        className={`group mx-3 my-2 px-4 py-3 rounded-xl border cursor-pointer transition-all ${
                          selectedEmail?.id === email.id ? 'bg-blue-50/70 border-blue-100 ring-1 ring-blue-200' : 'bg-white border-slate-100 hover:border-slate-200 hover:shadow-sm'
                        } ${!email.is_read ? 'border-blue-100/80' : ''}`}
                      >
                        <div className="flex items-start gap-3">
                          <div className={`relative w-10 h-10 rounded-full flex items-center justify-center text-sm font-semibold ${
                            !email.is_read ? 'bg-blue-100 text-blue-700' : 'bg-slate-100 text-slate-600'
                          }`}>
                            {email.from_name?.[0] || email.from?.[0]?.toUpperCase() || '?'}
                            {!email.is_read && (
                              <span className="absolute -right-1 -top-1 w-2.5 h-2.5 rounded-full bg-blue-500 ring-2 ring-white" />
                            )}
                          </div>
                          <div className="flex-1 min-w-0">
                            <div className="flex items-center gap-2">
                              <span className={`font-medium truncate ${!email.is_read ? 'text-slate-900' : 'text-slate-700'}`}>
                                {email.from_name || email.from}
                              </span>
                              {email.has_attachment && <Paperclip className="w-4 h-4 text-slate-400 flex-shrink-0" />}
                              <span className="ml-auto text-xs text-slate-400 group-hover:text-slate-500">
                                {new Date(email.date).toLocaleDateString('zh-CN')}
                              </span>
                            </div>
                            <p className={`text-sm truncate ${!email.is_read ? 'text-slate-900 font-semibold' : 'text-slate-500'}`}>
                              {email.subject || '(无主题)'}
                            </p>
                          </div>
                          <div className="flex items-center gap-1 opacity-0 translate-x-1 group-hover:opacity-100 group-hover:translate-x-0 transition">
                            {email.is_read ? (
                              <button
                                type="button"
                                onClick={(event) => {
                                  event.stopPropagation()
                                  handleMarkAsUnread(email)
                                }}
                                className="p-1.5 rounded-md text-slate-400 hover:text-slate-700 hover:bg-slate-100"
                                title="标记为未读"
                              >
                                <EyeOff className="w-4 h-4" />
                              </button>
                            ) : (
                              <button
                                type="button"
                                onClick={(event) => {
                                  event.stopPropagation()
                                  handleRead(email)
                                }}
                                className="p-1.5 rounded-md text-blue-500 hover:text-blue-700 hover:bg-blue-50"
                                title="标记为已读"
                              >
                                <Eye className="w-4 h-4" />
                              </button>
                            )}
                            <button
                              type="button"
                              onClick={(event) => {
                                event.stopPropagation()
                                requestDeleteEmail(email)
                              }}
                              className="p-1.5 rounded-md text-slate-400 hover:text-red-600 hover:bg-red-50"
                              title="删除"
                            >
                              <Trash2 className="w-4 h-4" />
                            </button>
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                ) : null
              ))}
            </div>
          )}
        </div>
      </div>
      <div className="flex-1 bg-white min-h-0">
        {selectedEmail ? (
          <div className="h-full flex flex-col">
            <div className="p-6 border-b border-slate-200/70 bg-gradient-to-b from-white to-slate-50/60 flex-shrink-0 relative">
              <div className="flex items-start gap-4 pr-16">
                <div className="min-w-0">
                  <h2 className="text-xl font-semibold text-slate-900 truncate">{selectedEmail.subject || '(无主题)'}</h2>
                  <div className="flex items-center gap-3 mt-3">
                    <div className="w-10 h-10 rounded-full bg-slate-100 flex items-center justify-center text-slate-600 font-semibold">
                      {selectedEmail.from_name?.[0] || '?'}
                    </div>
                    <div>
                      <p className="font-medium text-slate-900">{selectedEmail.from_name || selectedEmail.from}</p>
                      <p className="text-sm text-slate-500">{selectedEmail.from}</p>
                    </div>
                  </div>
                </div>
              </div>
              <div className="absolute top-5 right-6 flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => handleMarkAsUnread(selectedEmail)}
                  className="p-2 rounded-lg border border-slate-200 text-slate-600 hover:border-slate-300 hover:bg-slate-50"
                  title="标记未读"
                >
                  <EyeOff className="w-4 h-4" />
                </button>
                <button
                  type="button"
                  onClick={() => requestDeleteEmail(selectedEmail)}
                  className="p-2 rounded-lg border border-rose-200 text-rose-600 hover:bg-rose-50"
                  title="删除"
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
              <div className="mt-3 text-sm text-slate-400">
                {new Date(selectedEmail.date).toLocaleString('zh-CN')}
              </div>
            </div>
            <div ref={detailScrollRef} className="flex-1 p-6 overflow-auto text-slate-800 leading-relaxed">
              <div className="max-w-3xl mx-auto">
                {selectedEmail.body_html ? (
                  <div className="prose prose-slate max-w-none" dangerouslySetInnerHTML={{ __html: sanitizedHtml }} />
                ) : selectedEmail.body_text ? (
                  <pre className="whitespace-pre-wrap text-slate-800 leading-relaxed">{selectedEmail.body_text}</pre>
                ) : (
                  <p className="text-slate-500">无邮件内容</p>
                )}
              </div>
            </div>
          </div>
        ) : (
          <div className="h-full flex items-center justify-center text-slate-400">
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
