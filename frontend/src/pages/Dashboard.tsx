import { useState, useEffect } from 'react'
import { emailApi, accountApi } from '../api'
import type { Email, EmailAccount } from '../api'
import { Search, RefreshCw, Mail, Paperclip } from 'lucide-react'

export default function Dashboard() {
  const [emails, setEmails] = useState<Email[]>([])
  const [accounts, setAccounts] = useState<EmailAccount[]>([])
  const [loading, setLoading] = useState(false)
  const [selectedEmail, setSelectedEmail] = useState<Email | null>(null)
  const [search, setSearch] = useState('')

  useEffect(() => {
    loadAccounts()
    loadEmails()
  }, [])

  const loadAccounts = async () => {
    try {
      const res = await accountApi.list()
      setAccounts(res.data)
    } catch (e) {
      console.error(e)
    }
  }

  const loadEmails = async () => {
    setLoading(true)
    try {
      const res = await emailApi.list({ page: 1, page_size: 50, search })
      setEmails(res.data)
    } catch (e) {
      console.error(e)
    } finally {
      setLoading(false)
    }
  }

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

  return (
    <div className="h-full flex">
      <div className="w-96 border-r border-gray-200 bg-white flex flex-col">
        <div className="p-4 border-b border-gray-200">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
            <input
              type="text"
              placeholder="搜索邮件..."
              value={search}
              onChange={e => setSearch(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && loadEmails()}
              className="w-full pl-10 pr-4 py-2 border border-gray-200 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
          </div>
          <button
            onClick={handleSync}
            disabled={loading}
            className="mt-3 w-full flex items-center justify-center gap-2 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            同步邮件
          </button>
        </div>
        <div className="flex-1 overflow-auto">
          {emails.length === 0 ? (
            <div className="p-8 text-center text-gray-500">
              <Mail className="w-12 h-12 mx-auto mb-3 opacity-30" />
              <p>暂无邮件</p>
              <p className="text-sm mt-1">请先添加邮箱账户</p>
            </div>
          ) : (
            emails.map(email => (
              <div
                key={email.id}
                onClick={() => handleRead(email)}
                className={`p-4 border-b border-gray-100 cursor-pointer hover:bg-gray-50 ${
                  selectedEmail?.id === email.id ? 'bg-blue-50' : ''
                } ${!email.is_read ? 'bg-blue-50/50' : ''}`}
              >
                <div className="flex items-start gap-2">
                  <div className="w-10 h-10 rounded-full bg-gray-200 flex items-center justify-center text-gray-600 font-medium">
                    {email.from_name?.[0] || email.from?.[0]?.toUpperCase() || '?'}
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className={`font-medium truncate ${!email.is_read ? 'text-gray-900' : 'text-gray-600'}`}>
                        {email.from_name || email.from}
                      </span>
                      {email.has_attachment && <Paperclip className="w-4 h-4 text-gray-400 flex-shrink-0" />}
                    </div>
                    <p className={`text-sm truncate ${!email.is_read ? 'text-gray-900 font-medium' : 'text-gray-500'}`}>
                      {email.subject || '(无主题)'}
                    </p>
                    <p className="text-xs text-gray-400 mt-1">
                      {new Date(email.date).toLocaleDateString('zh-CN')}
                    </p>
                  </div>
                </div>
              </div>
            ))
          )}
        </div>
      </div>
      <div className="flex-1 bg-white">
        {selectedEmail ? (
          <div className="h-full flex flex-col">
            <div className="p-6 border-b border-gray-200">
              <h2 className="text-xl font-semibold">{selectedEmail.subject || '(无主题)'}</h2>
              <div className="flex items-center gap-3 mt-3">
                <div className="w-10 h-10 rounded-full bg-gray-200 flex items-center justify-center">
                  {selectedEmail.from_name?.[0] || '?'}
                </div>
                <div>
                  <p className="font-medium">{selectedEmail.from_name || selectedEmail.from}</p>
                  <p className="text-sm text-gray-500">{selectedEmail.from}</p>
                </div>
                <span className="ml-auto text-sm text-gray-400">
                  {new Date(selectedEmail.date).toLocaleString('zh-CN')}
                </span>
              </div>
            </div>
            <div className="flex-1 p-6 overflow-auto">
              {selectedEmail.body_html ? (
                <div dangerouslySetInnerHTML={{ __html: selectedEmail.body_html }} />
              ) : selectedEmail.body_text ? (
                <pre className="whitespace-pre-wrap">{selectedEmail.body_text}</pre>
              ) : (
                <p className="text-gray-500">无邮件内容</p>
              )}
            </div>
          </div>
        ) : (
          <div className="h-full flex items-center justify-center text-gray-400">
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