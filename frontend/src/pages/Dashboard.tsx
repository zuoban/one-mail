import { useState, useEffect, useCallback } from 'react'
import { emailApi, accountApi } from '../api'
import type { Email, EmailAccount } from '../api'
import { Search, RefreshCw, Mail, Paperclip } from 'lucide-react'

export default function Dashboard() {
  const [emails, setEmails] = useState<Email[]>([])
  const [accounts, setAccounts] = useState<EmailAccount[]>([])
  const [loading, setLoading] = useState(false)
  const [selectedEmail, setSelectedEmail] = useState<Email | null>(null)
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState<'all' | 'unread' | 'attachments'>('all')

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
      const res = await emailApi.list({ page: 1, page_size: 50, search })
      setEmails(res.data)
    } catch (e) {
      console.error(e)
    } finally {
      setLoading(false)
    }
  }, [search])

  useEffect(() => {
    loadAccounts()
    loadEmails()
  }, [loadAccounts, loadEmails])

  const handleSync = async () => {
    for (const account of accounts) {
      try {
        await accountApi.sync(account.id)
      } catch (e) {
        console.error(e)
      }
    }
    loadEmails()
  }

  const handleRead = async (email: Email) => {
    if (!email.is_read) {
      await emailApi.markAsRead(email.id)
      setEmails(emails.map(e => e.id === email.id ? { ...e, is_read: true } : e))
    }
    setSelectedEmail(email)
  }

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

  return (
    <div className="h-full flex bg-slate-50">
      <div className="w-96 border-r border-slate-200/70 bg-white flex flex-col">
        <div className="p-4 border-b border-slate-200/70 bg-gradient-to-b from-slate-50/80 to-white">
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2 text-slate-900">
              <Mail className="w-4 h-4 text-slate-500" />
              <span className="text-sm font-semibold">收件箱</span>
            </div>
            <span className="text-xs text-slate-500 bg-slate-100 px-2 py-0.5 rounded-full">
              {filteredEmails.length}
            </span>
          </div>
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
                        onClick={() => handleRead(email)}
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
        <div className="flex-1 bg-white">
          {selectedEmail ? (
            <div className="h-full flex flex-col">
              <div className="p-6 border-b border-slate-200/70 bg-gradient-to-b from-white to-slate-50/60">
                <h2 className="text-xl font-semibold text-slate-900">{selectedEmail.subject || '(无主题)'}</h2>
                <div className="flex items-center gap-3 mt-3">
                <div className="w-10 h-10 rounded-full bg-slate-100 flex items-center justify-center text-slate-600 font-semibold">
                  {selectedEmail.from_name?.[0] || '?'}
                </div>
                <div>
                  <p className="font-medium text-slate-900">{selectedEmail.from_name || selectedEmail.from}</p>
                  <p className="text-sm text-slate-500">{selectedEmail.from}</p>
                </div>
                <span className="ml-auto text-sm text-slate-400">
                  {new Date(selectedEmail.date).toLocaleString('zh-CN')}
                </span>
              </div>
            </div>
            <div className="flex-1 p-6 overflow-auto text-slate-800 leading-relaxed">
              {selectedEmail.body_html ? (
                <div dangerouslySetInnerHTML={{ __html: selectedEmail.body_html }} />
              ) : selectedEmail.body_text ? (
                <pre className="whitespace-pre-wrap text-slate-800 leading-relaxed">{selectedEmail.body_text}</pre>
              ) : (
                <p className="text-slate-500">无邮件内容</p>
              )}
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
