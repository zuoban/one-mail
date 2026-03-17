import { useState, useEffect } from 'react'
import axios from 'axios'
import { accountApi, syncApi } from '../api'
import type { EmailAccount, FolderStatus, SyncLog } from '../api'
import { Plus, Trash2, X, Mail, Plug, Pencil, FileText, RefreshCw, CheckCircle, XCircle, Loader2 } from 'lucide-react'
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
    enable_auto_sync: true,
  })
  const [error, setError] = useState('')
  const [syncingAccountId, setSyncingAccountId] = useState<number | null>(null)
  const [foldersAccount, setFoldersAccount] = useState<EmailAccount | null>(null)
  const [foldersLoading, setFoldersLoading] = useState(false)
  const [folders, setFolders] = useState<FolderStatus[]>([])
  const [selectedFolders, setSelectedFolders] = useState<string[]>([])
  const [showLogsDrawer, setShowLogsDrawer] = useState(false)
  const [syncLogs, setSyncLogs] = useState<SyncLog[]>([])
  const [logsLoading, setLogsLoading] = useState(false)

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
        const updateData: Partial<typeof form> = {}
        if (form.email !== editingAccount.email) updateData.email = form.email
        if (form.provider !== editingAccount.provider) updateData.provider = form.provider
        if (form.username !== editingAccount.username) updateData.username = form.username
        if (form.password) updateData.password = form.password
        if (form.color !== editingAccount.color) updateData.color = form.color
        if (form.enable_auto_sync !== (editingAccount.enable_auto_sync ?? true)) updateData.enable_auto_sync = form.enable_auto_sync
        
        await accountApi.update(editingAccount.id, updateData)
        setToast({ message: '账户更新成功', type: 'success' })
      } else {
        await accountApi.add(form)
        setToast({ message: '账户添加成功', type: 'success' })
      }
      setShowModal(false)
      setEditingAccount(null)
      setForm({ email: '', provider: 'gmail', username: '', password: '', color: '', enable_auto_sync: true })
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
    setForm({ email: '', provider: 'gmail', username: '', password: '', color: '', enable_auto_sync: true })
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
      enable_auto_sync: account.enable_auto_sync ?? true,
    })
    setError('')
    setShowModal(true)
  }

  const openFoldersModal = async (account: EmailAccount) => {
    setFoldersAccount(account)
    setFolders([])
    setSelectedFolders([])
    setFoldersLoading(true)
    setError('')
    try {
      const res = await accountApi.listFolders(account.id)
      setFolders(res.data)
      if (account.sync_folders) {
        const initial = account.sync_folders
          .split(',')
          .map(item => item.trim())
          .filter(Boolean)
        setSelectedFolders(initial)
      }
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setError(message || '获取文件夹失败')
    } finally {
      setFoldersLoading(false)
    }
  }

  const toggleFolder = (folder: string) => {
    setSelectedFolders(current => {
      if (current.includes(folder)) {
        return current.filter(item => item !== folder)
      }
      return [...current, folder]
    })
  }

  const applyFolders = async () => {
    if (!foldersAccount) return
    if (selectedFolders.length === 0) {
      setError('请选择至少一个文件夹')
      return
    }
    setLoading(true)
    setError('')
    try {
      const syncFolders = selectedFolders.join(',')
      await accountApi.updateSyncConfig(foldersAccount.id, { sync_folders: syncFolders })
      setToast({ message: '同步文件夹已更新', type: 'success' })
      setFoldersAccount(null)
      loadAccounts()
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setError(message || '更新失败')
    } finally {
      setLoading(false)
      setTimeout(() => setToast(null), 2000)
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

  const handleSync = async (id: number) => {
    if (syncingAccountId === id) return
    setSyncingAccountId(id)
    try {
      await accountApi.sync(id)
      setToast({ message: '同步已启动', type: 'success' })
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setToast({ message: message || '同步失败', type: 'error' })
    } finally {
      setTimeout(() => setSyncingAccountId(null), 2000)
      setTimeout(() => setToast(null), 1600)
    }
  }

  const openLogsDrawer = async (accountId: number) => {
    setShowLogsDrawer(true)
    loadSyncLogs(accountId)
  }

  const loadSyncLogs = async (accountId: number) => {
    setLogsLoading(true)
    try {
      const res = await syncApi.getLogsByAccount(accountId, { page: 1, page_size: 10 })
      setSyncLogs(res.data || [])
    } catch (e) {
      console.error(e)
    } finally {
      setLogsLoading(false)
    }
  }

  const formatTime = (time: string) => {
    if (!time) return '-'
    const date = new Date(time)
    return date.toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
  }

  const formatDuration = (ms: number) => {
    if (ms < 1000) return `${ms}ms`
    if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
    return `${(ms / 60000).toFixed(1)}min`
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
            <div
              key={account.id}
              className="group relative rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] p-4 transition-all duration-200 hover:border-[var(--primary-200)]"
            >

              <div className="flex items-center gap-5">

                {/* 头像 */}
                <div className="flex-shrink-0">
                  <div
                    className="w-12 h-12 rounded-xl flex items-center justify-center text-lg font-bold text-white"
                    style={{ backgroundColor: account.color || '#6366f1' }}
                  >
                    {account.email[0].toUpperCase()}
                  </div>
                </div>

                {/* 账户信息 */}
                <div className="flex-1 min-w-0">
                  <div className="mb-1">
                    <p className="font-medium text-[var(--text-primary)] truncate">{account.email}</p>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-[var(--text-tertiary)]">
                      {providers.find(p => p.value === account.provider)?.label}
                    </span>
                    <span className="text-[var(--text-tertiary)]">·</span>
                    <span className={`text-xs ${account.status === 'active' ? 'text-[var(--success-600)]' : 'text-[var(--text-tertiary)]'}`}>
                      {account.status === 'active' ? '已连接' : '未连接'}
                    </span>
                  </div>
                </div>

                {/* 操作按钮 */}
              <div className="flex items-center gap-1">
                  <button
                    onClick={() => openFoldersModal(account)}
                    className="p-2 rounded-lg text-[var(--text-tertiary)] hover:text-[var(--primary-600)] hover:bg-[var(--primary-50)] transition-colors"
                    title="同步文件夹"
                  >
                    <Mail className="w-4 h-4" />
                  </button>
                  <button
                    onClick={() => handleSync(account.id)}
                    disabled={syncingAccountId === account.id}
                    className="p-2 rounded-lg text-[var(--text-tertiary)] hover:text-[var(--primary-600)] hover:bg-[var(--primary-50)] transition-colors disabled:opacity-50"
                    title="手动同步"
                  >
                    <RefreshCw className={`w-4 h-4 ${syncingAccountId === account.id ? 'animate-spin' : ''}`} />
                  </button>
                  <button
                    onClick={() => openLogsDrawer(account.id)}
                    className="p-2 rounded-lg text-[var(--text-tertiary)] hover:text-[var(--primary-600)] hover:bg-[var(--primary-50)] transition-colors"
                    title="同步日志"
                  >
                    <FileText className="w-4 h-4" />
                  </button>
                  <button
                    onClick={() => handleTest(account.id)}
                    className="p-2 rounded-lg text-[var(--text-tertiary)] hover:text-[var(--primary-600)] hover:bg-[var(--primary-50)] transition-colors"
                    title="测试连接"
                  >
                    <Plug className="w-4 h-4" />
                  </button>
                  <button
                    onClick={() => openEditModal(account)}
                    className="p-2 rounded-lg text-[var(--text-tertiary)] hover:text-[var(--warning-600)] hover:bg-[var(--warning-50)] transition-colors"
                    title="编辑"
                  >
                    <Pencil className="w-4 h-4" />
                  </button>
                  <button
                    onClick={() => requestDelete(account)}
                    className="p-2 rounded-lg text-[var(--text-tertiary)] hover:text-[var(--error-600)] hover:bg-[var(--error-50)] transition-colors"
                    title="删除"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Folders Drawer */}
      {foldersAccount && (
        <>
          <div className="fixed inset-0 bg-black/50 z-40" onClick={() => setFoldersAccount(null)} />
          <div className="fixed right-0 top-0 bottom-0 w-[600px] bg-[var(--bg-primary)] shadow-2xl z-50 flex flex-col">
            <div className="flex items-center justify-between p-4 border-b border-[var(--border-light)]">
              <div>
                <h3 className="text-lg font-semibold text-[var(--text-primary)]">同步文件夹</h3>
                <p className="text-sm text-[var(--text-tertiary)]">{foldersAccount.email}</p>
              </div>
              <button
                type="button"
                onClick={() => setFoldersAccount(null)}
                className="p-2 rounded-lg hover:bg-[var(--bg-tertiary)] transition-colors"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            <div className="flex-1 overflow-auto p-6">
              {foldersLoading ? (
                <div className="flex items-center justify-center py-12">
                  <Loader2 className="w-8 h-8 animate-spin text-[var(--primary-500)]" />
                </div>
              ) : folders.length === 0 ? (
                <div className="text-center py-12">
                  <Mail className="w-12 h-12 mx-auto mb-3 text-[var(--text-tertiary)] opacity-30" />
                  <p className="text-[var(--text-secondary)]">没有获取到文件夹</p>
                </div>
              ) : (
                <div className="grid grid-cols-2 gap-2">
                  {folders.map(folder => (
                    <button
                      key={folder.name}
                      type="button"
                      onClick={() => toggleFolder(folder.name)}
                      className={`px-3 py-2 rounded-lg text-sm border transition-colors ${
                        selectedFolders.includes(folder.name)
                          ? 'bg-[var(--primary-50)] text-[var(--primary-700)] border-[var(--primary-200)]'
                          : 'bg-[var(--bg-secondary)] text-[var(--text-secondary)] border-[var(--border-light)] hover:bg-[var(--bg-tertiary)]'
                      }`}
                    >
                      <div className="flex flex-col items-start">
                        <span className="truncate max-w-[180px]">{folder.name}</span>
                        <span className="text-xs text-[var(--text-tertiary)]">
                          {folder.messages} 封 · 未读 {folder.unseen}
                        </span>
                      </div>
                    </button>
                  ))}
                </div>
              )}

              {error && (
                <div className="mt-4 flex items-center gap-2 p-3 rounded-lg bg-[var(--error-50)] border border-[var(--error-100)] text-[var(--error-600)] text-sm">
                  <X className="w-4 h-4 flex-shrink-0" />
                  <span>{error}</span>
                </div>
              )}
            </div>

            <div className="flex justify-end gap-3 p-4 border-t border-[var(--border-light)]">
              <button
                type="button"
                onClick={() => setFoldersAccount(null)}
                className="px-4 py-2 rounded-lg text-sm text-[var(--text-secondary)] hover:bg-[var(--bg-secondary)] transition-colors"
              >
                取消
              </button>
              <button
                type="button"
                onClick={applyFolders}
                disabled={loading || foldersLoading}
                className="px-4 py-2 rounded-lg text-white text-sm font-medium transition-colors disabled:opacity-50 bg-[var(--primary-500)] hover:bg-[var(--primary-600)]"
              >
                应用
              </button>
            </div>
          </div>
        </>
      )}

      {/* Add Account Drawer */}
      {showModal && (
        <>
          <div className="fixed inset-0 bg-black/50 z-40" onClick={() => {
            setShowModal(false)
            setEditingAccount(null)
          }} />
          <div className="fixed right-0 top-0 bottom-0 w-[600px] bg-[var(--bg-primary)] shadow-2xl z-50 flex flex-col">
            {/* Header */}
            <div className="flex items-center justify-between p-4 border-b border-[var(--border-light)]">
              <div className="flex items-center gap-3">
                <div className={`w-10 h-10 rounded-xl flex items-center justify-center shadow-lg ${
                  editingAccount
                    ? 'bg-gradient-to-br from-[var(--warning-500)] to-[var(--warning-600)] shadow-[var(--warning-500)]/25'
                    : 'bg-gradient-to-br from-[var(--primary-500)] to-[var(--primary-600)] shadow-[var(--primary-500)]/25'
                }`}>
                  <Mail className="w-5 h-5 text-white" />
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
                className="p-2 rounded-lg hover:bg-[var(--bg-tertiary)] transition-colors"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            {/* Form */}
            <form onSubmit={handleSubmit} className="flex-1 overflow-auto flex flex-col">
              <div className="flex-1 overflow-auto p-6">
              <div className="space-y-5">
                {/* Provider Selection */}
                <div>
                  <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                    邮箱类型
                  </label>
                  <div className="grid grid-cols-3 gap-2">
                    {providers.map(p => (
                      <button
                        key={p.value}
                        type="button"
                        onClick={() => setForm({ ...form, provider: p.value })}
                        className={`px-3 py-2 rounded-lg text-sm transition-colors ${
                          form.provider === p.value
                            ? 'bg-[var(--primary-500)] text-white'
                            : 'bg-[var(--bg-secondary)] text-[var(--text-secondary)] hover:bg-[var(--bg-tertiary)]'
                        }`}
                      >
                        {p.label}
                      </button>
                    ))}
                  </div>
                </div>

                {/* Email Address */}
                <div>
                  <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                    邮箱地址
                  </label>
                  <input
                    type="email"
                    value={form.email}
                    onChange={e => setForm({ ...form, email: e.target.value })}
                    placeholder="example@gmail.com"
                    required
                    className="w-full px-4 py-2.5 rounded-lg bg-[var(--bg-secondary)] border border-[var(--border-light)] text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] focus:outline-none focus:ring-2 focus:ring-[var(--primary-500)] focus:border-transparent transition-colors"
                  />
                </div>

                {/* Username */}
                <div>
                  <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                    用户名
                  </label>
                  <input
                    type="text"
                    value={form.username}
                    onChange={e => setForm({ ...form, username: e.target.value })}
                    placeholder="通常是邮箱地址"
                    required
                    className="w-full px-4 py-2.5 rounded-lg bg-[var(--bg-secondary)] border border-[var(--border-light)] text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] focus:outline-none focus:ring-2 focus:ring-[var(--primary-500)] focus:border-transparent transition-colors"
                  />
                </div>

                {/* Password */}
                <div>
                  <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                    密码/授权码
                    {editingAccount && <span className="text-xs text-[var(--text-tertiary)] font-normal ml-1">(可选)</span>}
                  </label>
                  <input
                    type="password"
                    value={form.password}
                    onChange={e => setForm({ ...form, password: e.target.value })}
                    placeholder={editingAccount ? "留空表示保持原密码不变" : "应用专用密码或授权码"}
                    required={!editingAccount}
                    className="w-full px-4 py-2.5 rounded-lg bg-[var(--bg-secondary)] border border-[var(--border-light)] text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] focus:outline-none focus:ring-2 focus:ring-[var(--primary-500)] focus:border-transparent transition-colors"
                  />
                  <p className="mt-1 text-xs text-[var(--text-tertiary)]">
                    {editingAccount ? '如需更改密码，请输入新密码' : '建议使用应用专用密码'}
                  </p>
                </div>

                {/* Color Selection */}
                <div>
                  <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                    账户颜色
                  </label>
                  <div className="flex items-center gap-2">
                    {accountColors.map(color => (
                      <button
                        key={color}
                        type="button"
                        onClick={() => setForm({ ...form, color })}
                        className={`w-7 h-7 rounded-full transition-transform hover:scale-110 ${
                          form.color === color ? 'ring-2 ring-offset-2 ring-[var(--primary-500)]' : ''
                        }`}
                        style={{ backgroundColor: color }}
                      />
                    ))}
                    <button
                      type="button"
                      onClick={() => setForm({ ...form, color: '' })}
                      className={`px-3 py-1.5 rounded-lg text-xs transition-colors ${
                        form.color === ''
                          ? 'bg-[var(--primary-500)] text-white'
                          : 'bg-[var(--bg-secondary)] text-[var(--text-tertiary)] hover:bg-[var(--bg-tertiary)]'
                      }`}
                    >
                      随机
                    </button>
                  </div>
                </div>

                {/* Auto Sync Toggle */}
                <div className="flex items-center justify-between">
                  <div>
                    <label className="block text-sm font-medium text-[var(--text-secondary)]">
                      自动同步
                    </label>
                    <p className="text-xs text-[var(--text-tertiary)] mt-0.5">
                      开启后每分钟自动同步邮件
                    </p>
                  </div>
                  <button
                    type="button"
                    onClick={() => setForm({ ...form, enable_auto_sync: !form.enable_auto_sync })}
                    className={`relative w-11 h-6 rounded-full transition-colors ${
                      form.enable_auto_sync ? 'bg-[var(--primary-500)]' : 'bg-[var(--bg-tertiary)]'
                    }`}
                  >
                    <span
                      className={`absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white shadow transition-transform ${
                        form.enable_auto_sync ? 'translate-x-5' : 'translate-x-0'
                      }`}
                    />
                  </button>
                </div>

                {/* Error Message */}
                {error && (
                  <div className="flex items-center gap-2 p-3 rounded-lg bg-[var(--error-50)] border border-[var(--error-100)] text-[var(--error-600)] text-sm animate-in slide-in-from-top-2">
                    <X className="w-4 h-4 flex-shrink-0" />
                    <span>{error}</span>
                  </div>
                )}
              </div>

              </div>

              {/* Footer */}
              <div className="flex justify-end gap-3 p-4 border-t border-[var(--border-light)]">
                <button
                  type="button"
                  onClick={() => {
                    setShowModal(false)
                    setEditingAccount(null)
                  }}
                  className="px-4 py-2 rounded-lg text-sm text-[var(--text-secondary)] hover:bg-[var(--bg-secondary)] transition-colors"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={loading}
                  className={`px-4 py-2 rounded-lg text-white text-sm font-medium transition-colors disabled:opacity-50 ${
                    editingAccount
                      ? 'bg-[var(--warning-500)] hover:bg-[var(--warning-600)]'
                      : 'bg-[var(--primary-500)] hover:bg-[var(--primary-600)]'
                  }`}
                >
                  {loading ? '处理中...' : (editingAccount ? '保存' : '添加')}
                </button>
              </div>
            </form>
          </div>
        </>
      )}

      {/* Sync Logs Drawer */}
      {showLogsDrawer && (
        <>
          <div className="fixed inset-0 bg-black/50 z-40" onClick={() => setShowLogsDrawer(false)} />
          <div className="fixed right-0 top-0 bottom-0 w-[600px] bg-[var(--bg-primary)] shadow-2xl z-50 flex flex-col">
            <div className="flex items-center justify-between p-4 border-b border-[var(--border-light)]">
              <h3 className="text-lg font-semibold text-[var(--text-primary)]">同步日志</h3>
              <button onClick={() => setShowLogsDrawer(false)} className="p-2 rounded-lg hover:bg-[var(--bg-tertiary)] transition-colors">
                <X className="w-5 h-5" />
              </button>
            </div>
            <div className="flex-1 overflow-auto p-4">
              {logsLoading ? (
                <div className="flex items-center justify-center py-12">
                  <Loader2 className="w-8 h-8 animate-spin text-[var(--primary-500)]" />
                </div>
              ) : syncLogs.length === 0 ? (
                <div className="text-center py-12">
                  <FileText className="w-12 h-12 mx-auto mb-3 text-[var(--text-tertiary)] opacity-30" />
                  <p className="text-[var(--text-secondary)]">暂无同步日志</p>
                </div>
              ) : (
                <div className="space-y-3">
                  {syncLogs.map(log => (
                    <div key={log.id} className="bg-[var(--bg-secondary)] rounded-lg p-4 border border-[var(--border-light)]">
                      <div className="flex items-center justify-between mb-2">
                        <div className="flex items-center gap-2">
                          {log.status === 'success' ? (
                            <CheckCircle className="w-4 h-4 text-[var(--success-600)]" />
                          ) : log.status === 'failed' ? (
                            <XCircle className="w-4 h-4 text-[var(--error-600)]" />
                          ) : (
                            <Loader2 className="w-4 h-4 text-[var(--primary-600)] animate-spin" />
                          )}
                          <span className={`text-sm font-medium ${
                            log.status === 'success' ? 'text-[var(--success-600)]' :
                            log.status === 'failed' ? 'text-[var(--error-600)]' :
                            'text-[var(--primary-600)]'
                          }`}>
                            {log.status === 'success' ? '成功' : log.status === 'failed' ? '失败' : '进行中'}
                          </span>
                        </div>
                        <span className="text-xs text-[var(--text-tertiary)]">{formatTime(log.start_time)}</span>
                      </div>
                      <div className="flex items-center gap-4 text-xs text-[var(--text-secondary)]">
                        <span>耗时: {formatDuration(log.duration_ms)}</span>
                        <span>新增: {log.new_count} 封</span>
                      </div>
                      {log.error && (
                        <div className="mt-2 text-xs text-[var(--error-600)] bg-[var(--error-50)] rounded p-2">
                          {log.error}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </>
      )}
    </div>
  )
}
