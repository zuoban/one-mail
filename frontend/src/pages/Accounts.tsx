import { useState, useEffect, useRef } from 'react'
import axios from 'axios'
import { accountApi } from '../api'
import type { EmailAccount } from '../api'
import { Plus, Trash2, X, Mail, Plug, Pencil, RefreshCw } from 'lucide-react'
import ConfirmDialog from '../components/ConfirmDialog'
import Tooltip from '../components/Tooltip'

const providers = [
  { value: 'gmail', label: 'Gmail' },
  { value: 'qq', label: 'QQ邮箱' },
  { value: 'outlook', label: 'Outlook' },
  { value: 'qq-work', label: 'QQ企业邮' },
  { value: '163', label: '163邮箱' },
  { value: 'custom', label: '自定义' },
]

const accountColors = [
  '#2563eb',
  '#059669',
  '#d97706',
  '#dc2626',
  '#0891b2',
  '#4f46e5',
  '#db2777',
  '#65a30d',
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
  const [usernameEdited, setUsernameEdited] = useState(false)
  const [refreshingAccountId, setRefreshingAccountId] = useState<number | null>(null)
  const drawerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    loadAccounts()
  }, [])

  useEffect(() => {
    if (!showModal) return

    const drawer = drawerRef.current
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    drawer?.focus()

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setShowModal(false)
        setEditingAccount(null)
        return
      }
      if (event.key !== 'Tab' || !drawer) return

      const controls = drawer.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled])')
      const first = controls[0]
      const last = controls[controls.length - 1]
      if (!first || !last) {
        event.preventDefault()
        return
      }
      if (event.shiftKey && (document.activeElement === first || document.activeElement === drawer)) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => {
      window.removeEventListener('keydown', handleKeyDown)
      previousFocus?.focus()
    }
  }, [showModal])

  const loadAccounts = async () => {
    try {
      const res = await accountApi.list()
      setAccounts(res.data)
    } catch (e) {
      console.error(e)
    }
  }

  const showToast = (message: string, type: 'success' | 'error') => {
    setToast({ message, type })
    window.setTimeout(() => setToast(null), 2000)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      if (editingAccount) {
        const updateData: Partial<typeof form> = {}
        if (form.email !== editingAccount.email) updateData.email = form.email
        if (form.provider !== editingAccount.provider) updateData.provider = form.provider
        if (form.username !== editingAccount.username) updateData.username = form.username
        if (form.password) updateData.password = form.password
        if (form.color !== editingAccount.color) updateData.color = form.color

        await accountApi.update(editingAccount.id, updateData)
        showToast('账户已更新', 'success')
      } else {
        await accountApi.add(form)
        showToast('账户已添加', 'success')
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
    }
  }

  const openAddModal = () => {
    setEditingAccount(null)
    setUsernameEdited(false)
    setForm({ email: '', provider: 'gmail', username: '', password: '', color: '' })
    setError('')
    setShowModal(true)
  }

  const openEditModal = (account: EmailAccount) => {
    setEditingAccount(account)
    setUsernameEdited(true)
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

  const confirmDeleteAccount = async () => {
    if (!confirmDelete) return
    await accountApi.delete(confirmDelete.id)
    setConfirmDelete(null)
    showToast('账户已删除', 'success')
    loadAccounts()
  }

  const handleTest = async (id: number) => {
    try {
      await accountApi.test(id)
      showToast('连接测试通过', 'success')
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      showToast(message || '连接失败', 'error')
    }
  }

  const handleRefresh = async (id: number) => {
    if (refreshingAccountId === id) return
    setRefreshingAccountId(id)
    try {
      const res = await accountApi.refresh(id)
      showToast(res.new_count > 0 ? `已刷新，新增 ${res.new_count} 封邮件` : '已是最新邮件', 'success')
      loadAccounts()
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      showToast(message || '刷新失败', 'error')
    } finally {
      setRefreshingAccountId(null)
    }
  }

  const formatTime = (time: string) => {
    if (!time) return '未刷新'
    const date = new Date(time)
    if (Number.isNaN(date.getTime()) || date.getFullYear() < 1970) return '未刷新'
    return date.toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    })
  }
  return (
    <div className="workspace-page">
      <ConfirmDialog
        open={Boolean(confirmDelete)}
        title="删除账户"
        message={confirmDelete ? `确定要删除 ${confirmDelete.email} 吗？` : '确定要删除该账户吗？'}
        confirmLabel="删除"
        cancelLabel="取消"
        onConfirm={confirmDeleteAccount}
        onCancel={() => setConfirmDelete(null)}
      />

      {/* Toast */}
      {toast && (
        <div className="fixed left-4 right-4 top-4 z-50 md:left-auto md:right-4">
          <div
            role="status"
            className={`flex items-center gap-3 px-4 py-3 rounded-[8px] text-sm border shadow-[var(--shadow-md)] ${
              toast.type === 'success'
                ? 'bg-[var(--bg-primary)] text-[var(--text-primary)] border-[var(--border-default)]'
                : 'bg-[var(--error-50)] text-[var(--error-700)] border-[var(--error-100)]'
            }`}
          >
            <span className={`status-dot ${toast.type === 'success' ? 'status-dot-success' : 'status-dot-error'}`} />
            <span>{toast.message}</span>
          </div>
        </div>
      )}

      {/* Header */}
      <div className="page-heading flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1>邮箱账户</h1>
          <p>
            管理已接入的 IMAP 邮箱，按需刷新拉取最近 50 封邮件
          </p>
        </div>
        <button
          type="button"
          onClick={openAddModal}
          className="btn btn-primary w-full sm:w-auto"
        >
          <Plus className="w-4 h-4" strokeWidth={1.5} />
          <span>添加账户</span>
        </button>
      </div>

      {/* Account List */}
      {accounts.length === 0 ? (
        <div className="rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] shadow-[var(--shadow-sm)] empty-state">
          <Mail className="empty-state-icon" strokeWidth={1.5} />
          <p className="text-base font-medium text-[var(--text-primary)]">暂无邮箱账户</p>
          <p className="text-sm mt-1 text-[var(--text-tertiary)]">点击右上角添加你的第一个邮箱账户</p>
        </div>
      ) : (
        <div className="rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] shadow-[var(--shadow-sm)] divide-y divide-[var(--border-light)] overflow-hidden">
          {accounts.map(account => (
            <div
              key={account.id}
              className="p-4 md:p-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between hover:bg-[var(--bg-nav-hover)] transition-colors"
            >
              <div className="flex items-center gap-4 min-w-0">
                <div
                  className="relative w-10 h-10 rounded-lg border border-[var(--border-light)] bg-[var(--bg-secondary)] flex items-center justify-center text-sm font-medium text-[var(--text-secondary)] flex-shrink-0"
                  aria-hidden="true"
                >
                  {account.email[0].toUpperCase()}
                  <span
                    className="absolute -bottom-1 -right-1 h-3 w-3 rounded-full border-2 border-[var(--bg-primary)]"
                    style={{ backgroundColor: account.color || '#2563EB' }}
                  />
                </div>

                <div className="min-w-0">
                  <div className="flex items-center gap-2 min-w-0">
                    <p className="text-sm font-medium text-[var(--text-primary)] truncate">
                      {account.email}
                    </p>
                  </div>

                  <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-[var(--text-tertiary)]">
                    <span>{providers.find(p => p.value === account.provider)?.label || account.provider}</span>
                    <span className="inline-flex items-center gap-2">
                      <span className={`status-dot ${account.status === 'active' ? 'status-dot-success' : 'status-dot-warning'}`} />
                      <span className={account.status === 'active' ? 'text-[var(--text-secondary)]' : ''}>
                        {account.status === 'active' ? '已连接' : '未连接'}
                      </span>
                    </span>
                    <span className="font-mono tabular-nums">
                      上次刷新: {formatTime(account.last_sync_time)}
                    </span>
                  </div>
                </div>
              </div>

              <div className="flex flex-wrap items-center gap-2 flex-shrink-0">
                <button
                  type="button"
                  onClick={() => handleRefresh(account.id)}
                  disabled={refreshingAccountId === account.id}
                  className="btn btn-secondary btn-sm"
                >
                  <RefreshCw className={`w-4 h-4 ${refreshingAccountId === account.id ? 'animate-spin' : ''}`} strokeWidth={1.5} />
                  <span>刷新</span>
                </button>

                <div className="flex items-center gap-1 ml-1 pl-2 border-l border-[var(--border-light)]">
                  <Tooltip content="测试连接">
                    <button
                      type="button"
                      onClick={() => handleTest(account.id)}
                      className="btn btn-ghost w-9 h-9 p-0"
                      aria-label="测试连接"
                    >
                      <Plug className="w-4 h-4 text-[var(--text-tertiary)]" strokeWidth={1.5} />
                    </button>
                  </Tooltip>
                  <Tooltip content="编辑账户">
                    <button
                      type="button"
                      onClick={() => openEditModal(account)}
                      className="btn btn-ghost w-9 h-9 p-0"
                      aria-label="编辑账户"
                    >
                      <Pencil className="w-4 h-4 text-[var(--text-tertiary)]" strokeWidth={1.5} />
                    </button>
                  </Tooltip>
                  <Tooltip content="删除账户">
                    <button
                      type="button"
                      onClick={() => setConfirmDelete(account)}
                      className="btn btn-ghost w-9 h-9 p-0 hover:text-[var(--error-600)] hover:bg-[var(--error-50)]"
                      aria-label="删除账户"
                    >
                      <Trash2 className="w-4 h-4" strokeWidth={1.5} />
                    </button>
                  </Tooltip>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}


      {/* Add / Edit Account Drawer */}
      {showModal && (
        <>
          <div
            className="overlay-backdrop fixed inset-0 z-40"
            onClick={() => {
              setShowModal(false)
              setEditingAccount(null)
            }}
          />
          <div
            ref={drawerRef}
            role="dialog"
            aria-modal="true"
            aria-labelledby="account-drawer-title"
            aria-describedby="account-drawer-description"
            tabIndex={-1}
            className="fixed inset-y-0 right-0 w-full sm:w-[480px] bg-[var(--bg-primary)] border-l border-[var(--border-default)] shadow-[var(--shadow-xl)] z-50 flex flex-col"
          >
            <div className="h-16 px-6 border-b border-[var(--border-light)] flex items-center justify-between">
              <div className="min-w-0">
                <h2 id="account-drawer-title" className="text-base font-medium text-[var(--text-primary)]">
                  {editingAccount ? '编辑邮箱账户' : '添加邮箱账户'}
                </h2>
                <p id="account-drawer-description" className="text-xs text-[var(--text-tertiary)] truncate">
                  {editingAccount ? editingAccount.email : '配置 IMAP 邮箱接入信息'}
                </p>
              </div>
              <button
                type="button"
                onClick={() => {
                  setShowModal(false)
                  setEditingAccount(null)
                }}
                className="btn btn-ghost w-9 h-9 p-0"
                aria-label="关闭"
              >
                <X className="w-4 h-4" strokeWidth={1.5} />
              </button>
            </div>

            <form onSubmit={handleSubmit} className="flex-1 overflow-auto flex flex-col">
              <div className="flex-1 overflow-auto p-6 space-y-6 thin-scrollbar">
                <fieldset className="min-w-0">
                  <legend className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                    邮箱服务商
                  </legend>
                  <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
                    {providers.map(p => (
                      <button
                        key={p.value}
                        type="button"
                        onClick={() => setForm({ ...form, provider: p.value })}
                        aria-pressed={form.provider === p.value}
                        className={`btn h-9 px-3 rounded-[8px] text-sm border transition-colors cursor-pointer ${
                          form.provider === p.value
                            ? 'bg-[var(--bg-accent-soft)] text-[var(--text-primary)] font-medium border-[var(--primary-200)]'
                            : 'bg-transparent text-[var(--text-secondary)] border-[var(--border-light)] hover:border-[var(--border-hover)]'
                        }`}
                      >
                        {p.label}
                      </button>
                    ))}
                  </div>
                </fieldset>

                <div>
                  <label htmlFor="account-email" className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                    邮箱地址
                  </label>
                  <input
                    id="account-email"
                    type="email"
                    value={form.email}
                    onChange={e => {
                      const email = e.target.value
                      setForm(prev => ({
                        ...prev,
                        email,
                        username: !editingAccount && !usernameEdited ? email : prev.username,
                      }))
                    }}
                    placeholder="name@example.com"
                    required
                  />
                </div>

                <div>
                  <label htmlFor="account-username" className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                    登录用户名
                  </label>
                  <input
                    id="account-username"
                    type="text"
                    value={form.username}
                    onChange={e => {
                      setUsernameEdited(true)
                      setForm({ ...form, username: e.target.value })
                    }}
                    placeholder="通常与邮箱地址一致"
                    required
                  />
                </div>

                <div>
                  <label htmlFor="account-password" className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                    密码 / 授权码
                    {editingAccount && (
                      <span id="account-password-help" className="text-xs text-[var(--text-tertiary)] font-normal ml-2">留空则保持不变</span>
                    )}
                  </label>
                  <input
                    id="account-password"
                    aria-describedby={editingAccount ? 'account-password-help' : undefined}
                    type="password"
                    value={form.password}
                    onChange={e => setForm({ ...form, password: e.target.value })}
                    placeholder={editingAccount ? '留空保持原密码' : '输入应用专用密码或授权码'}
                    required={!editingAccount}
                  />
                </div>

                <fieldset className="min-w-0">
                  <legend className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                    账户标识色
                  </legend>
                  <div className="flex flex-wrap items-center gap-2">
                    {accountColors.map(color => (
                      <button
                        key={color}
                        type="button"
                        onClick={() => setForm({ ...form, color })}
                        className={`btn btn-ghost w-9 h-9 p-0 rounded-lg border transition-colors cursor-pointer ${
                          form.color === color
                            ? 'bg-[var(--bg-secondary)] border-[var(--text-secondary)]'
                            : 'border-transparent hover:border-[var(--border-hover)]'
                        }`}
                        aria-label={`选择颜色 ${color}`}
                        aria-pressed={form.color === color}
                      >
                        <span aria-hidden="true" className="h-4 w-4 rounded-full" style={{ backgroundColor: color }} />
                      </button>
                    ))}
                    <button
                      type="button"
                      onClick={() => setForm({ ...form, color: '' })}
                      aria-pressed={form.color === ''}
                      className={`btn h-9 px-3 rounded-[8px] text-sm border transition-colors cursor-pointer ${
                        form.color === ''
                          ? 'bg-[var(--bg-tertiary)] text-[var(--text-primary)] font-medium border-[var(--border-default)]'
                          : 'bg-transparent text-[var(--text-tertiary)] border-[var(--border-light)] hover:border-[var(--border-hover)]'
                      }`}
                    >
                      自动
                    </button>
                  </div>
                </fieldset>


                {error && (
                  <div role="alert" className="alert alert-error flex items-center gap-2">
                    <X className="w-4 h-4 flex-shrink-0" strokeWidth={1.5} />
                    <span>{error}</span>
                  </div>
                )}
              </div>

              <div className="flex flex-col-reverse sm:flex-row sm:justify-end gap-2 px-6 py-4 border-t border-[var(--border-light)]">
                <button
                  type="button"
                  onClick={() => {
                    setShowModal(false)
                    setEditingAccount(null)
                  }}
                  className="btn btn-secondary w-full sm:w-auto"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={loading}
                  className="btn btn-primary w-full sm:w-auto"
                >
                  {loading ? '处理中' : editingAccount ? '保存修改' : '添加账户'}
                </button>
              </div>
            </form>
          </div>
        </>
      )}

    </div>
  )
}
