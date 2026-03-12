import { useState, useEffect } from 'react'
import axios from 'axios'
import { accountApi } from '../api'
import type { EmailAccount } from '../api'
import { Plus, Trash2, RefreshCw, Check, X, Mail, Plug } from 'lucide-react'
import ConfirmDialog from '../components/ConfirmDialog'

const providers = [
  { value: 'gmail', label: 'Gmail' },
  { value: 'qq', label: 'QQ邮箱' },
  { value: 'outlook', label: 'Outlook' },
  { value: 'qq-work', label: 'QQ企业邮' },
  { value: '163', label: '163邮箱' },
  { value: 'custom', label: '自定义' },
]

export default function Accounts() {
  const [accounts, setAccounts] = useState<EmailAccount[]>([])
  const [loading, setLoading] = useState(false)
  const [showModal, setShowModal] = useState(false)
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<EmailAccount | null>(null)
  const [form, setForm] = useState({
    email: '',
    provider: 'gmail',
    username: '',
    password: '',
  })
  const [error, setError] = useState('')

  useEffect(() => {
    loadAccounts()
  }, [])

  const loadAccounts = async () => {
    try {
      const res = await accountApi.list()
      setAccounts(res.data)
    } catch (e) {
      console.error(e)
    }
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      await accountApi.add(form)
      setShowModal(false)
      setForm({ email: '', provider: 'gmail', username: '', password: '' })
      loadAccounts()
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setError(message || '添加失败')
    } finally {
      setLoading(false)
    }
  }

  const requestDelete = (account: EmailAccount) => {
    setConfirmDelete(account)
  }

  const confirmDeleteAccount = async () => {
    if (!confirmDelete) return
    await accountApi.delete(confirmDelete.id)
    setConfirmDelete(null)
    loadAccounts()
  }

  const cancelDeleteAccount = () => {
    setConfirmDelete(null)
  }

  const handleSync = async (id: number) => {
    setLoading(true)
    try {
      await accountApi.sync(id)
      setToast({ message: '同步成功', type: 'success' })
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setToast({ message: message || '同步失败', type: 'error' })
    } finally {
      setLoading(false)
      setTimeout(() => setToast(null), 1600)
    }
  }

  const handleTest = async (id: number) => {
    try {
      await accountApi.test(id)
      setToast({ message: '连接成功', type: 'success' })
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setToast({ message: message || '连接失败', type: 'error' })
    } finally {
      setTimeout(() => setToast(null), 1600)
    }
  }

  return (
    <div className="p-6">
      <ConfirmDialog
        open={Boolean(confirmDelete)}
        title="确认删除"
        message={confirmDelete ? `确定要删除 ${confirmDelete.email} 吗？` : '确定要删除该账户吗？'}
        confirmLabel="删除"
        cancelLabel="取消"
        onConfirm={confirmDeleteAccount}
        onCancel={cancelDeleteAccount}
      />
      {toast && (
        <div className="fixed top-4 right-4 z-50">
          <div className={`px-4 py-2 rounded-lg text-sm shadow-lg border ${toast.type === 'success' ? 'bg-emerald-50 text-emerald-700 border-emerald-200' : 'bg-rose-50 text-rose-700 border-rose-200'}`}>
            {toast.message}
          </div>
        </div>
      )}
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-2xl font-bold text-gray-800">邮箱账户</h2>
        <button
          onClick={() => setShowModal(true)}
          className="flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700"
        >
          <Plus className="w-4 h-4" />
          添加账户
        </button>
      </div>

      {accounts.length === 0 ? (
        <div className="text-center py-16 text-gray-500">
          <Mail className="w-16 h-16 mx-auto mb-4 opacity-30" />
          <p className="text-lg">暂无邮箱账户</p>
          <p className="text-sm mt-2">点击"添加账户"来绑定你的邮箱</p>
        </div>
      ) : (
        <div className="grid gap-4">
          {accounts.map(account => (
            <div key={account.id} className="bg-white border border-gray-200 rounded-lg p-4 flex items-center gap-4">
              <div className="w-12 h-12 rounded-full bg-blue-100 flex items-center justify-center text-blue-600 font-bold">
                {account.email[0].toUpperCase()}
              </div>
              <div className="flex-1">
                <p className="font-medium text-gray-900">{account.email}</p>
                <p className="text-sm text-gray-500">{providers.find(p => p.value === account.provider)?.label}</p>
              </div>
              <div className="flex items-center gap-2">
                <span className={`flex items-center gap-1 text-sm ${account.status === 'active' ? 'text-green-600' : 'text-gray-400'}`}>
                  {account.status === 'active' ? <Check className="w-4 h-4" /> : <X className="w-4 h-4" />}
                  {account.status === 'active' ? '已连接' : '未连接'}
                </span>
                <button
                  onClick={() => handleTest(account.id)}
                  className="p-2 text-gray-400 hover:text-gray-600"
                  title="测试连接"
                >
                  <Plug className="w-4 h-4" />
                </button>
                <button
                  onClick={() => handleSync(account.id)}
                  className="p-2 text-gray-400 hover:text-blue-600"
                  title="同步邮件"
                >
                  <RefreshCw className="w-4 h-4" />
                </button>
                <button
                  onClick={() => requestDelete(account)}
                  className="p-2 text-gray-400 hover:text-red-600"
                  title="删除"
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {showModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg p-6 w-full max-w-md">
            <h3 className="text-lg font-semibold mb-4">添加邮箱账户</h3>
            <form onSubmit={handleSubmit}>
              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">邮箱类型</label>
                  <select
                    value={form.provider}
                    onChange={e => setForm({ ...form, provider: e.target.value })}
                    className="w-full px-3 py-2 border border-gray-200 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
                  >
                    {providers.map(p => (
                      <option key={p.value} value={p.value}>{p.label}</option>
                    ))}
                  </select>
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">邮箱地址</label>
                  <input
                    type="email"
                    value={form.email}
                    onChange={e => setForm({ ...form, email: e.target.value })}
                    className="w-full px-3 py-2 border border-gray-200 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
                    required
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">用户名</label>
                  <input
                    type="text"
                    value={form.username}
                    onChange={e => setForm({ ...form, username: e.target.value })}
                    className="w-full px-3 py-2 border border-gray-200 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
                    placeholder="通常是邮箱地址"
                    required
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">密码/授权码</label>
                  <input
                    type="password"
                    value={form.password}
                    onChange={e => setForm({ ...form, password: e.target.value })}
                    className="w-full px-3 py-2 border border-gray-200 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
                    placeholder="应用专用密码或授权码"
                    required
                  />
                </div>
                {error && <p className="text-red-500 text-sm">{error}</p>}
              </div>
              <div className="flex justify-end gap-3 mt-6">
                <button
                  type="button"
                  onClick={() => setShowModal(false)}
                  className="px-4 py-2 text-gray-600 hover:bg-gray-100 rounded-lg"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={loading}
                  className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
                >
                  {loading ? '添加中...' : '添加'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
