import { useState, useEffect } from 'react'
import axios from 'axios'
import { accountApi } from '../api'
import type { EmailAccount } from '../api'
import { Plus, Trash2, Check, X, Mail, Plug, X as CloseIcon, Mail as MailIcon, User, Lock, Palette, Pencil } from 'lucide-react'
import ConfirmDialog from '../components/ConfirmDialog'

const providers = [
  { value: 'gmail', label: 'Gmail' },
  { value: 'qq', label: 'QQ邮箱' },
  { value: 'outlook', label: 'Outlook' },
  { value: 'qq-work', label: 'QQ企业邮' },
  { value: '163', label: '163邮箱' },
  { value: 'custom', label: '自定义' },
]

const accountColors = [
  '#6366f1', // indigo
  '#8b5cf6', // violet
  '#ec4899', // pink
  '#f43f5e', // rose
  '#f97316', // orange
  '#eab308', // yellow
  '#22c55e', // green
  '#14b8a6', // teal
  '#06b6d4', // cyan
  '#3b82f6', // blue
]

export default function Accounts() {
  const [accounts, setAccounts] = useState<EmailAccount[]>([])
  const [loading, setLoading] = useState(false)
  const [showModal, setShowModal] = useState(false)
  const [editingAccount, setEditingAccount] = useState<EmailAccount | null>(null)
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<EmailAccount | null>(null)
  const [form, setForm] = useState({
    email: '',
    provider: 'gmail',
    username: '',
    password: '',
    color: '',
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
      if (editingAccount) {
        // 编辑模式 - 只发送变更的字段
        const updateData: Partial<typeof form> = {}
        if (form.email !== editingAccount.email) updateData.email = form.email
        if (form.provider !== editingAccount.provider) updateData.provider = form.provider
        if (form.username !== editingAccount.username) updateData.username = form.username
        if (form.password) updateData.password = form.password
        if (form.color !== editingAccount.color) updateData.color = form.color
        
        await accountApi.update(editingAccount.id, updateData)
        setToast({ message: '账户更新成功', type: 'success' })
      } else {
        // 添加模式
        await accountApi.add(form)
        setToast({ message: '账户添加成功', type: 'success' })
      }
      setShowModal(false)
      setEditingAccount(null)
      setForm({ email: '', provider: 'gmail', username: '', password: '', color: '' })
      loadAccounts()
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setError(message || (editingAccount ? '更新失败' : '添加失败'))
    } finally {
      setLoading(false)
      setTimeout(() => setToast(null), 2000)
    }
  }

  const openAddModal = () => {
    setEditingAccount(null)
    setForm({ email: '', provider: 'gmail', username: '', password: '', color: '' })
    setError('')
    setShowModal(true)
  }

  const openEditModal = (account: EmailAccount) => {
    setEditingAccount(account)
    setForm({
      email: account.email,
      provider: account.provider,
      username: account.username,
      password: '',
      color: account.color || '',
    })
    setError('')
    setShowModal(true)
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
          onClick={openAddModal}
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
              <div 
                className="w-10 h-10 rounded-full flex items-center justify-center text-sm font-semibold text-white flex-shrink-0"
                style={{ backgroundColor: account.color || '#6366f1' }}
              >
                {account.email[0].toUpperCase()}
              </div>
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <p className="font-medium text-[var(--text-primary)] truncate">{account.email}</p>
                  <div 
                    className="w-2.5 h-2.5 rounded-full flex-shrink-0 ring-2 ring-white dark:ring-[var(--bg-primary)]"
                    style={{ backgroundColor: account.color || '#6366f1' }}
                    title="账户颜色"
                  />
                </div>
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
                  onClick={() => openEditModal(account)}
                  className="btn btn-ghost p-2 hover:text-[var(--primary-600)] hover:bg-[var(--primary-50)]"
                  title="编辑"
                >
                  <Pencil className="w-4 h-4" />
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
        <div 
          className="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center z-50 p-4 animate-in fade-in duration-200"
          onClick={() => {
            setShowModal(false)
            setEditingAccount(null)
          }}
        >
          <div 
            className="bg-white dark:bg-[var(--bg-primary)] rounded-2xl shadow-2xl w-full max-w-lg animate-in zoom-in-95 duration-200"
            onClick={e => e.stopPropagation()}
          >
            {/* Header */}
            <div className="flex items-center justify-between px-6 py-5 border-b border-[var(--border-default)]">
              <div className="flex items-center gap-3">
                <div className={`w-10 h-10 rounded-xl flex items-center justify-center shadow-lg ${
                  editingAccount 
                    ? 'bg-gradient-to-br from-[var(--warning-500)] to-[var(--warning-600)] shadow-[var(--warning-500)]/25'
                    : 'bg-gradient-to-br from-[var(--primary-500)] to-[var(--primary-600)] shadow-[var(--primary-500)]/25'
                }`}>
                  <MailIcon className="w-5 h-5 text-white" />
                </div>
                <div>
                  <h3 className="text-lg font-semibold text-[var(--text-primary)]">
                    {editingAccount ? '编辑邮箱账户' : '添加邮箱账户'}
                  </h3>
                  <p className="text-sm text-[var(--text-tertiary)]">
                    {editingAccount ? `编辑 ${editingAccount.email}` : '连接您的邮件服务'}
                  </p>
                </div>
              </div>
              <button
                type="button"
                onClick={() => {
                  setShowModal(false)
                  setEditingAccount(null)
                }}
                className="p-2 rounded-lg hover:bg-[var(--bg-secondary)] text-[var(--text-tertiary)] hover:text-[var(--text-secondary)] transition-colors"
              >
                <CloseIcon className="w-5 h-5" />
              </button>
            </div>

            {/* Form */}
            <form onSubmit={handleSubmit} className="p-6">
              <div className="space-y-5">
                {/* Provider Selection */}
                <div>
                  <label className="flex items-center gap-2 text-sm font-medium text-[var(--text-secondary)] mb-2.5">
                    <MailIcon className="w-4 h-4" />
                    邮箱类型
                  </label>
                  <div className="grid grid-cols-3 gap-2">
                    {providers.slice(0, -1).map(p => (
                      <button
                        key={p.value}
                        type="button"
                        onClick={() => setForm({ ...form, provider: p.value })}
                        className={`px-3 py-2.5 rounded-lg text-sm font-medium transition-all duration-200 ${
                          form.provider === p.value
                            ? 'bg-[var(--primary-50)] text-[var(--primary-600)] border-2 border-[var(--primary-500)]'
                            : 'bg-[var(--bg-secondary)] text-[var(--text-secondary)] border-2 border-transparent hover:border-[var(--border-default)]'
                        }`}
                      >
                        {p.label}
                      </button>
                    ))}
                    <button
                      type="button"
                      onClick={() => setForm({ ...form, provider: 'custom' })}
                      className={`px-3 py-2.5 rounded-lg text-sm font-medium transition-all duration-200 ${
                        form.provider === 'custom'
                          ? 'bg-[var(--primary-50)] text-[var(--primary-600)] border-2 border-[var(--primary-500)]'
                          : 'bg-[var(--bg-secondary)] text-[var(--text-secondary)] border-2 border-transparent hover:border-[var(--border-default)]'
                      }`}
                    >
                      自定义
                    </button>
                  </div>
                </div>

                {/* Email Address */}
                <div>
                  <label className="flex items-center gap-2 text-sm font-medium text-[var(--text-secondary)] mb-2.5">
                    <MailIcon className="w-4 h-4" />
                    邮箱地址
                  </label>
                  <div className="relative">
                    <input
                      type="email"
                      value={form.email}
                      onChange={e => setForm({ ...form, email: e.target.value })}
                      placeholder="example@gmail.com"
                      required
                      className="w-full px-4 py-3 rounded-xl bg-[var(--bg-secondary)] border border-[var(--border-default)] text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] focus:outline-none focus:ring-2 focus:ring-[var(--primary-500)] focus:border-transparent transition-all"
                    />
                  </div>
                </div>

                {/* Username */}
                <div>
                  <label className="flex items-center gap-2 text-sm font-medium text-[var(--text-secondary)] mb-2.5">
                    <User className="w-4 h-4" />
                    用户名
                  </label>
                  <div className="relative">
                    <input
                      type="text"
                      value={form.username}
                      onChange={e => setForm({ ...form, username: e.target.value })}
                      placeholder="通常是邮箱地址"
                      required
                      className="w-full px-4 py-3 rounded-xl bg-[var(--bg-secondary)] border border-[var(--border-default)] text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] focus:outline-none focus:ring-2 focus:ring-[var(--primary-500)] focus:border-transparent transition-all"
                    />
                  </div>
                </div>

                {/* Password */}
                <div>
                  <label className="flex items-center gap-2 text-sm font-medium text-[var(--text-secondary)] mb-2.5">
                    <Lock className="w-4 h-4" />
                    密码/授权码
                    {editingAccount && <span className="text-xs text-[var(--text-tertiary)] font-normal">(可选)</span>}
                  </label>
                  <div className="relative">
                    <input
                      type="password"
                      value={form.password}
                      onChange={e => setForm({ ...form, password: e.target.value })}
                      placeholder={editingAccount ? "留空表示保持原密码不变" : "应用专用密码或授权码"}
                      required={!editingAccount}
                      className="w-full px-4 py-3 rounded-xl bg-[var(--bg-secondary)] border border-[var(--border-default)] text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] focus:outline-none focus:ring-2 focus:ring-[var(--primary-500)] focus:border-transparent transition-all"
                    />
                  </div>
                  <p className="mt-1.5 text-xs text-[var(--text-tertiary)]">
                    {editingAccount 
                      ? '如需更改密码，请输入新密码；否则留空保持原密码'
                      : '为了账户安全，建议使用应用专用密码而非登录密码'
                    }
                  </p>
                </div>

                {/* Color Selection */}
                <div>
                  <label className="flex items-center gap-2 text-sm font-medium text-[var(--text-secondary)] mb-2.5">
                    <Palette className="w-4 h-4" />
                    账户颜色
                  </label>
                  <div className="flex items-center gap-3">
                    <div className="flex gap-2 flex-wrap">
                      {accountColors.map(color => (
                        <button
                          key={color}
                          type="button"
                          onClick={() => setForm({ ...form, color })}
                          className={`w-8 h-8 rounded-full transition-all duration-200 hover:scale-110 ${
                            form.color === color 
                              ? 'ring-2 ring-offset-2 ring-[var(--primary-500)] scale-110' 
                              : 'hover:ring-2 hover:ring-offset-1 hover:ring-[var(--border-default)]'
                          }`}
                          style={{ backgroundColor: color }}
                        />
                      ))}
                    </div>
                    <div className="h-8 w-px bg-[var(--border-default)] mx-2" />
                    <button
                      type="button"
                      onClick={() => setForm({ ...form, color: '' })}
                      className={`px-3 py-2 rounded-lg text-xs font-medium transition-all duration-200 ${
                        form.color === '' 
                          ? 'bg-[var(--primary-500)] text-white shadow-lg shadow-[var(--primary-500)]/25' 
                          : 'bg-[var(--bg-secondary)] text-[var(--text-tertiary)] hover:bg-[var(--bg-tertiary)]'
                      }`}
                    >
                      随机
                    </button>
                  </div>
                </div>

                {/* Error Message */}
                {error && (
                  <div className="flex items-center gap-2 p-3 rounded-lg bg-[var(--error-50)] border border-[var(--error-100)] text-[var(--error-600)] text-sm animate-in slide-in-from-top-2">
                    <X className="w-4 h-4 flex-shrink-0" />
                    <span>{error}</span>
                  </div>
                )}
              </div>

              {/* Footer */}
              <div className="flex justify-end gap-3 mt-6 pt-6 border-t border-[var(--border-default)]">
                <button
                  type="button"
                  onClick={() => {
                    setShowModal(false)
                    setEditingAccount(null)
                  }}
                  className="px-5 py-2.5 rounded-xl text-sm font-medium text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-secondary)] transition-colors"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={loading}
                  className={`px-5 py-2.5 rounded-xl text-white text-sm font-medium shadow-lg hover:shadow-xl hover:scale-[1.02] active:scale-[0.98] transition-all duration-200 disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:scale-100 flex items-center gap-2 ${
                    editingAccount 
                      ? 'bg-gradient-to-r from-[var(--warning-500)] to-[var(--warning-600)] shadow-[var(--warning-500)]/25 hover:shadow-[var(--warning-500)]/30'
                      : 'bg-gradient-to-r from-[var(--primary-500)] to-[var(--primary-600)] shadow-[var(--primary-500)]/25 hover:shadow-[var(--primary-500)]/30'
                  }`}
                >
                  {loading ? (
                    <>
                      <svg className="animate-spin h-4 w-4" viewBox="0 0 24 24">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" fill="none" />
                        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
                      </svg>
                      {editingAccount ? '保存中...' : '添加中...'}
                    </>
                  ) : (
                    <>
                      {editingAccount ? <Pencil className="w-4 h-4" /> : <Plus className="w-4 h-4" />}
                      {editingAccount ? '保存修改' : '添加账户'}
                    </>
                  )}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}
