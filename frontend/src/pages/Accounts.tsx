import { useState, useEffect } from 'react'
import axios from 'axios'
import { accountApi } from '../api'
import type { EmailAccount } from '../api'
import { Plus, Trash2, Check, X, Mail, Plug } from 'lucide-react'
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

      {/* Toast */}
      {toast && (
        <div className="fixed top-4 right-4 z-50">
          <div className={`px-4 py-3 rounded-lg text-sm shadow-lg border ${toast.type === 'success' ? 'bg-[var(--success-50)] text-[var(--success-600)] border-[var(--success-100)]' : 'bg-[var(--error-50)] text-[var(--error-600)] border-[var(--error-100)]'}`}>
            {toast.message}
          </div>
        </div>
      )}

      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-2xl font-semibold text-[var(--text-primary)]">邮箱账户</h2>
        <button
          onClick={() => setShowModal(true)}
          className="btn btn-primary"
        >
          <Plus className="w-4 h-4" />
          <span>添加账户</span>
        </button>
      </div>

      {/* Empty State */}
      {accounts.length === 0 ? (
        <div className="empty-state">
          <Mail className="empty-state-icon" />
          <p className="text-lg text-[var(--text-secondary)]">暂无邮箱账户</p>
          <p className="text-sm mt-2 text-[var(--text-tertiary)]">点击"添加账户"来绑定你的邮箱</p>
        </div>
      ) : (
        <div className="grid gap-4">
          {accounts.map(account => (
            <div key={account.id} className="card-static p-4 flex items-center gap-4">
              <div className="avatar">
                {account.email[0].toUpperCase()}
              </div>
              <div className="flex-1 min-w-0">
                <p className="font-medium text-[var(--text-primary)] truncate">{account.email}</p>
                <p className="text-sm text-[var(--text-secondary)]">{providers.find(p => p.value === account.provider)?.label}</p>
              </div>
              <div className="flex items-center gap-2">
                <span className={`flex items-center gap-1 text-sm ${account.status === 'active' ? 'text-[var(--success-600)]' : 'text-[var(--text-tertiary)]'}`}>
                  {account.status === 'active' ? <Check className="w-4 h-4" /> : <X className="w-4 h-4" />}
                  {account.status === 'active' ? '已连接' : '未连接'}
                </span>
                <button
                  onClick={() => handleTest(account.id)}
                  className="btn btn-ghost p-2"
                  title="测试连接"
                >
                  <Plug className="w-4 h-4" />
                </button>
                <button
                  onClick={() => requestDelete(account)}
                  className="btn btn-ghost p-2 hover:text-[var(--error-600)] hover:bg-[var(--error-50)]"
                  title="删除"
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Add Account Modal */}
      {showModal && (
        <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50 p-4">
          <div className="card-static w-full max-w-md">
            <h3 className="text-lg font-semibold text-[var(--text-primary)] mb-4">添加邮箱账户</h3>
            <form onSubmit={handleSubmit}>
              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">邮箱类型</label>
                  <select
                    value={form.provider}
                    onChange={e => setForm({ ...form, provider: e.target.value })}
                  >
                    {providers.map(p => (
                      <option key={p.value} value={p.value}>{p.label}</option>
                    ))}
                  </select>
                </div>
                <div>
                  <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">邮箱地址</label>
                  <input
                    type="email"
                    value={form.email}
                    onChange={e => setForm({ ...form, email: e.target.value })}
                    required
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">用户名</label>
                  <input
                    type="text"
                    value={form.username}
                    onChange={e => setForm({ ...form, username: e.target.value })}
                    placeholder="通常是邮箱地址"
                    required
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">密码/授权码</label>
                  <input
                    type="password"
                    value={form.password}
                    onChange={e => setForm({ ...form, password: e.target.value })}
                    placeholder="应用专用密码或授权码"
                    required
                  />
                </div>
                {error && <p className="text-[var(--error-600)] text-sm">{error}</p>}
              </div>
              <div className="flex justify-end gap-3 mt-6">
                <button
                  type="button"
                  onClick={() => setShowModal(false)}
                  className="btn btn-secondary"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={loading}
                  className="btn btn-primary"
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
