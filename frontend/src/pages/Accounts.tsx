import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import axios from 'axios'
import { accountApi, syncApi } from '../api'
import type { EmailAccount, FolderStatus, SyncLog } from '../api'
import { Plus, Trash2, X, Mail, FolderOpen, Plug, Pencil, FileText, RefreshCw, CheckCircle, XCircle, Loader2, ArrowUpRight, Check } from 'lucide-react'
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
    enable_auto_sync: true,
  })
  const [error, setError] = useState('')
  const [usernameEdited, setUsernameEdited] = useState(false)
  const [syncingAccountId, setSyncingAccountId] = useState<number | null>(null)
  const [foldersAccount, setFoldersAccount] = useState<EmailAccount | null>(null)
  const [foldersLoading, setFoldersLoading] = useState(false)
  const [folders, setFolders] = useState<FolderStatus[]>([])
  const [selectedFolders, setSelectedFolders] = useState<string[]>([])
  const [logsAccount, setLogsAccount] = useState<EmailAccount | null>(null)
  const [syncLogs, setSyncLogs] = useState<SyncLog[]>([])
  const [logsLoading, setLogsLoading] = useState(false)

  useEffect(() => {
    loadAccounts()
  }, [])

  useEffect(() => {
    const handleEsc = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      if (foldersAccount) setFoldersAccount(null)
      else if (showModal) {
        setShowModal(false)
        setEditingAccount(null)
      } else if (logsAccount) setLogsAccount(null)
    }
    window.addEventListener('keydown', handleEsc)
    return () => window.removeEventListener('keydown', handleEsc)
  }, [foldersAccount, showModal, logsAccount])

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
        if (form.enable_auto_sync !== (editingAccount.enable_auto_sync ?? true)) {
          updateData.enable_auto_sync = form.enable_auto_sync
        }

        await accountApi.update(editingAccount.id, updateData)
        showToast('账户已更新', 'success')
      } else {
        await accountApi.add(form)
        showToast('账户已添加', 'success')
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
    }
  }

  const openAddModal = () => {
    setEditingAccount(null)
    setUsernameEdited(false)
    setForm({ email: '', provider: 'gmail', username: '', password: '', color: '', enable_auto_sync: true })
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
        setSelectedFolders(
          account.sync_folders
            .split(',')
            .map(item => item.trim())
            .filter(Boolean),
        )
      }
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setError(message || '获取文件夹失败')
    } finally {
      setFoldersLoading(false)
    }
  }

  const toggleFolder = (folder: string) => {
    setSelectedFolders(current =>
      current.includes(folder)
        ? current.filter(item => item !== folder)
        : [...current, folder],
    )
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
      await accountApi.updateSyncConfig(foldersAccount.id, { sync_folders: selectedFolders.join(',') })
      showToast('同步文件夹已更新', 'success')
      setFoldersAccount(null)
      loadAccounts()
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setError(message || '更新失败')
    } finally {
      setLoading(false)
    }
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

  const handleSync = async (id: number) => {
    if (syncingAccountId === id) return
    setSyncingAccountId(id)
    try {
      await accountApi.sync(id)
      showToast('已启动同步', 'success')
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      showToast(message || '同步失败', 'error')
    } finally {
      window.setTimeout(() => setSyncingAccountId(null), 1600)
    }
  }

  const openLogsDrawer = async (account: EmailAccount) => {
    setLogsAccount(account)
    setLogsLoading(true)
    try {
      const res = await syncApi.getLogsByAccount(account.id, { page: 1, page_size: 10 })
      setSyncLogs(res.data || [])
    } catch (e) {
      console.error(e)
    } finally {
      setLogsLoading(false)
    }
  }

  const formatTime = (time: string) => {
    if (!time) return '未同步'
    const date = new Date(time)
    if (Number.isNaN(date.getTime())) return '未同步'
    return date.toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    })
  }

  const formatDuration = (ms: number) => {
    if (ms < 1000) return `${ms}ms`
    if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
    return `${(ms / 60000).toFixed(1)}m`
  }

  return (
    <div className="max-w-5xl mx-auto p-4 md:p-8">
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
            className={`flex items-center gap-2.5 px-4 py-2.5 rounded-[8px] text-sm border shadow-[var(--shadow-md)] ${
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
      <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-medium tracking-[-0.015em] text-[var(--text-primary)]">邮箱账户</h1>
          <p className="mt-1 text-sm text-[var(--text-secondary)]">
            管理已接入的 IMAP 邮箱，配置同步文件夹与自动拉取
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
          <Mail className="empty-state-icon" />
          <p className="text-base font-medium text-[var(--text-primary)]">暂无邮箱账户</p>
          <p className="text-sm mt-1 text-[var(--text-tertiary)]">点击右上角添加你的第一个邮箱账户</p>
        </div>
      ) : (
        <div className="rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] shadow-[var(--shadow-sm)] divide-y divide-[var(--border-light)] overflow-hidden">
          {accounts.map(account => (
            <div
              key={account.id}
              className="p-4 md:p-5 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between hover:bg-[var(--bg-nav-hover)] transition-colors"
            >
              <div className="flex items-center gap-4 min-w-0">
                <div
                  className="w-10 h-10 rounded-[10px] flex items-center justify-center text-sm font-medium text-white flex-shrink-0"
                  style={{ backgroundColor: account.color || '#2563EB' }}
                >
                  {account.email[0].toUpperCase()}
                </div>

                <div className="min-w-0">
                  <div className="flex items-center gap-2 min-w-0">
                    <p className="text-sm font-medium text-[var(--text-primary)] truncate">
                      {account.email}
                    </p>
                    {account.sync_folders && (() => {
                      const folderList = account.sync_folders.split(',').map(f => f.trim()).filter(Boolean)
                      return folderList.length > 0 ? (
                        <div className="hidden md:flex items-center gap-1">
                          {folderList.slice(0, 2).map(folder => (
                            <span
                              key={folder}
                              className="inline-flex px-1.5 py-0.5 rounded-[4px] bg-[var(--bg-tertiary)] border border-[var(--border-light)] font-mono text-[11px] text-[var(--text-tertiary)]"
                            >
                              {folder}
                            </span>
                          ))}
                          {folderList.length > 2 && (
                            <span className="inline-flex px-1.5 py-0.5 rounded-[4px] bg-[var(--bg-tertiary)] border border-[var(--border-light)] font-mono tabular-nums text-[11px] text-[var(--text-tertiary)]">
                              +{folderList.length - 2}
                            </span>
                          )}
                        </div>
                      ) : null
                    })()}
                  </div>

                  <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-[var(--text-tertiary)]">
                    <span>{providers.find(p => p.value === account.provider)?.label || account.provider}</span>
                    <span className="inline-flex items-center gap-1.5">
                      <span className={`status-dot ${account.status === 'active' ? 'status-dot-success' : 'status-dot-warning'}`} />
                      <span className={account.status === 'active' ? 'text-[var(--text-secondary)]' : ''}>
                        {account.status === 'active' ? '已连接' : '未连接'}
                      </span>
                    </span>
                    <span className="font-mono tabular-nums">
                      同步: {formatTime(account.last_sync_time)}
                    </span>
                    <span>{account.enable_auto_sync === false ? '手动同步' : '自动同步'}</span>
                  </div>
                </div>
              </div>

              <div className="flex flex-wrap items-center gap-1.5 flex-shrink-0">
                <button
                  type="button"
                  onClick={() => handleSync(account.id)}
                  disabled={syncingAccountId === account.id}
                  className="btn btn-secondary btn-sm"
                >
                  <RefreshCw className={`w-3.5 h-3.5 ${syncingAccountId === account.id ? 'animate-spin' : ''}`} strokeWidth={1.5} />
                  <span>同步</span>
                </button>
                <button
                  type="button"
                  onClick={() => openFoldersModal(account)}
                  className="btn btn-secondary btn-sm"
                >
                  <FolderOpen className="w-3.5 h-3.5" strokeWidth={1.5} />
                  <span>文件夹</span>
                </button>

                <div className="flex items-center gap-0.5 ml-1 pl-2 border-l border-[var(--border-light)]">
                  <Tooltip content="同步日志">
                    <button
                      type="button"
                      onClick={() => openLogsDrawer(account)}
                      className="btn btn-ghost w-8 h-8 p-0"
                      aria-label="同步日志"
                    >
                      <FileText className="w-4 h-4 text-[var(--text-tertiary)]" strokeWidth={1.5} />
                    </button>
                  </Tooltip>
                  <Tooltip content="测试连接">
                    <button
                      type="button"
                      onClick={() => handleTest(account.id)}
                      className="btn btn-ghost w-8 h-8 p-0"
                      aria-label="测试连接"
                    >
                      <Plug className="w-4 h-4 text-[var(--text-tertiary)]" strokeWidth={1.5} />
                    </button>
                  </Tooltip>
                  <Tooltip content="编辑账户">
                    <button
                      type="button"
                      onClick={() => openEditModal(account)}
                      className="btn btn-ghost w-8 h-8 p-0"
                      aria-label="编辑账户"
                    >
                      <Pencil className="w-4 h-4 text-[var(--text-tertiary)]" strokeWidth={1.5} />
                    </button>
                  </Tooltip>
                  <Tooltip content="删除账户">
                    <button
                      type="button"
                      onClick={() => setConfirmDelete(account)}
                      className="btn btn-ghost w-8 h-8 p-0 hover:text-[var(--error-600)] hover:bg-[var(--error-50)]"
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

      {/* Folders Drawer */}
      {foldersAccount && (
        <>
          <div className="overlay-backdrop fixed inset-0 z-40" onClick={() => setFoldersAccount(null)} />
          <div className="fixed inset-y-0 right-0 w-full sm:w-[480px] bg-[var(--bg-primary)] border-l border-[var(--border-default)] shadow-[var(--shadow-xl)] z-50 flex flex-col">
            <div className="h-16 px-6 border-b border-[var(--border-light)] flex items-center justify-between">
              <div className="min-w-0">
                <h3 className="text-base font-medium text-[var(--text-primary)]">同步文件夹</h3>
                <p className="text-xs font-mono text-[var(--text-tertiary)] truncate">{foldersAccount.email}</p>
              </div>
              <button
                type="button"
                onClick={() => setFoldersAccount(null)}
                className="btn btn-ghost w-8 h-8 p-0"
                aria-label="关闭"
              >
                <X className="w-4 h-4" strokeWidth={1.5} />
              </button>
            </div>

            <div className="flex-1 overflow-auto p-6 thin-scrollbar">
              {foldersLoading ? (
                <div className="flex items-center justify-center py-12 gap-2 text-sm text-[var(--text-tertiary)]">
                  <Loader2 className="w-4 h-4 animate-spin" strokeWidth={1.5} />
                  <span>获取文件夹列表</span>
                </div>
              ) : folders.length === 0 ? (
                <div className="empty-state py-12">
                  <FolderOpen className="empty-state-icon" />
                  <p className="text-sm text-[var(--text-secondary)]">没有获取到文件夹</p>
                </div>
              ) : (
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                  {folders.map(folder => {
                    const selected = selectedFolders.includes(folder.name)
                    return (
                      <button
                        key={folder.name}
                        type="button"
                        onClick={() => toggleFolder(folder.name)}
                        className={`px-3 py-2.5 rounded-[8px] text-left border transition-colors cursor-pointer ${
                          selected
                            ? 'bg-[var(--bg-accent-soft)] text-[var(--text-primary)] border-[var(--primary-200)]'
                            : 'bg-[var(--bg-secondary)] text-[var(--text-secondary)] border-[var(--border-light)] hover:border-[var(--border-hover)]'
                        }`}
                      >
                        <div className="flex items-center justify-between gap-2">
                          <span className="text-sm font-medium truncate">{folder.name}</span>
                          {selected && (
                            <Check className="w-3.5 h-3.5 text-[var(--primary-500)] flex-shrink-0" strokeWidth={2} />
                          )}
                        </div>
                        <div className="mt-0.5 font-mono tabular-nums text-xs text-[var(--text-tertiary)]">
                          {folder.messages} 封 · 未读 {folder.unseen}
                        </div>
                      </button>
                    )
                  })}
                </div>
              )}

              {error && (
                <div className="alert alert-error mt-4 flex items-center gap-2">
                  <X className="w-4 h-4 flex-shrink-0" strokeWidth={1.5} />
                  <span>{error}</span>
                </div>
              )}
            </div>

            <div className="flex flex-col-reverse sm:flex-row sm:justify-end gap-2 px-6 py-4 border-t border-[var(--border-light)]">
              <button
                type="button"
                onClick={() => setFoldersAccount(null)}
                className="btn btn-secondary w-full sm:w-auto"
              >
                取消
              </button>
              <button
                type="button"
                onClick={applyFolders}
                disabled={loading || foldersLoading}
                className="btn btn-primary w-full sm:w-auto"
              >
                {loading ? '保存中' : '保存文件夹'}
              </button>
            </div>
          </div>
        </>
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
          <div className="fixed inset-y-0 right-0 w-full sm:w-[480px] bg-[var(--bg-primary)] border-l border-[var(--border-default)] shadow-[var(--shadow-xl)] z-50 flex flex-col">
            <div className="h-16 px-6 border-b border-[var(--border-light)] flex items-center justify-between">
              <div className="min-w-0">
                <h3 className="text-base font-medium text-[var(--text-primary)]">
                  {editingAccount ? '编辑邮箱账户' : '添加邮箱账户'}
                </h3>
                <p className="text-xs text-[var(--text-tertiary)] truncate">
                  {editingAccount ? editingAccount.email : '配置 IMAP 邮箱接入信息'}
                </p>
              </div>
              <button
                type="button"
                onClick={() => {
                  setShowModal(false)
                  setEditingAccount(null)
                }}
                className="btn btn-ghost w-8 h-8 p-0"
                aria-label="关闭"
              >
                <X className="w-4 h-4" strokeWidth={1.5} />
              </button>
            </div>

            <form onSubmit={handleSubmit} className="flex-1 overflow-auto flex flex-col">
              <div className="flex-1 overflow-auto p-6 space-y-5 thin-scrollbar">
                <div>
                  <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                    邮箱服务商
                  </label>
                  <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
                    {providers.map(p => (
                      <button
                        key={p.value}
                        type="button"
                        onClick={() => setForm({ ...form, provider: p.value })}
                        className={`h-9 px-3 rounded-[8px] text-sm border transition-colors cursor-pointer ${
                          form.provider === p.value
                            ? 'bg-[var(--bg-accent-soft)] text-[var(--text-primary)] font-medium border-[var(--primary-200)]'
                            : 'bg-transparent text-[var(--text-secondary)] border-[var(--border-light)] hover:border-[var(--border-hover)]'
                        }`}
                      >
                        {p.label}
                      </button>
                    ))}
                  </div>
                </div>

                <div>
                  <label className="block text-sm font-medium text-[var(--text-secondary)] mb-1.5">
                    邮箱地址
                  </label>
                  <input
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
                  <label className="block text-sm font-medium text-[var(--text-secondary)] mb-1.5">
                    登录用户名
                  </label>
                  <input
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
                  <label className="block text-sm font-medium text-[var(--text-secondary)] mb-1.5">
                    密码 / 授权码
                    {editingAccount && (
                      <span className="text-xs text-[var(--text-tertiary)] font-normal ml-1.5">留空则保持不变</span>
                    )}
                  </label>
                  <input
                    type="password"
                    value={form.password}
                    onChange={e => setForm({ ...form, password: e.target.value })}
                    placeholder={editingAccount ? '留空保持原密码' : '输入应用专用密码或授权码'}
                    required={!editingAccount}
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                    账户标识色
                  </label>
                  <div className="flex flex-wrap items-center gap-2">
                    {accountColors.map(color => (
                      <button
                        key={color}
                        type="button"
                        onClick={() => setForm({ ...form, color })}
                        className={`w-6 h-6 rounded-full shadow-[inset_0_0_0_1px_rgba(255,255,255,0.14)] transition-shadow cursor-pointer ${
                          form.color === color
                            ? 'ring-2 ring-[var(--text-primary)] ring-offset-2 ring-offset-[var(--bg-primary)]'
                            : ''
                        }`}
                        style={{ backgroundColor: color }}
                        aria-label={`选择颜色 ${color}`}
                      />
                    ))}
                    <button
                      type="button"
                      onClick={() => setForm({ ...form, color: '' })}
                      className={`h-6 px-2.5 rounded-[6px] text-xs border transition-colors cursor-pointer ${
                        form.color === ''
                          ? 'bg-[var(--bg-tertiary)] text-[var(--text-primary)] font-medium border-[var(--border-default)]'
                          : 'bg-transparent text-[var(--text-tertiary)] border-[var(--border-light)] hover:border-[var(--border-hover)]'
                      }`}
                    >
                      自动
                    </button>
                  </div>
                </div>

                <div className="flex items-center justify-between pt-2 border-t border-[var(--border-light)]">
                  <div>
                    <span className="block text-sm font-medium text-[var(--text-primary)]">自动同步</span>
                    <span className="block text-xs text-[var(--text-tertiary)] mt-0.5">每分钟自动拉取新邮件</span>
                  </div>
                  <button
                    type="button"
                    role="switch"
                    aria-checked={form.enable_auto_sync}
                    onClick={() => setForm({ ...form, enable_auto_sync: !form.enable_auto_sync })}
                    className={`relative w-11 h-6 rounded-full transition-colors cursor-pointer ${
                      form.enable_auto_sync ? 'bg-[var(--primary-500)]' : 'bg-[var(--bg-tertiary)] shadow-[inset_0_0_0_1px_var(--border-default)]'
                    }`}
                  >
                    <span
                      className={`toggle-thumb absolute top-0.5 left-0.5 w-5 h-5 rounded-full transition-transform ${
                        form.enable_auto_sync ? 'translate-x-5' : 'translate-x-0'
                      }`}
                    />
                  </button>
                </div>

                {error && (
                  <div className="alert alert-error flex items-center gap-2">
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

      {/* Sync Logs Drawer */}
      {logsAccount && (
        <>
          <div className="overlay-backdrop fixed inset-0 z-40" onClick={() => setLogsAccount(null)} />
          <div className="fixed inset-y-0 right-0 w-full sm:w-[480px] bg-[var(--bg-primary)] border-l border-[var(--border-default)] shadow-[var(--shadow-xl)] z-50 flex flex-col">
            <div className="h-16 px-6 border-b border-[var(--border-light)] flex items-center justify-between">
              <div className="min-w-0">
                <h3 className="text-base font-medium text-[var(--text-primary)]">同步日志</h3>
                <p className="text-xs font-mono text-[var(--text-tertiary)] truncate">{logsAccount.email}</p>
              </div>
              <button
                type="button"
                onClick={() => setLogsAccount(null)}
                className="btn btn-ghost w-8 h-8 p-0"
                aria-label="关闭"
              >
                <X className="w-4 h-4" strokeWidth={1.5} />
              </button>
            </div>

            <div className="flex-1 overflow-auto p-6 thin-scrollbar">
              {logsLoading ? (
                <div className="flex items-center justify-center py-12 gap-2 text-sm text-[var(--text-tertiary)]">
                  <Loader2 className="w-4 h-4 animate-spin" strokeWidth={1.5} />
                  <span>加载日志</span>
                </div>
              ) : syncLogs.length === 0 ? (
                <div className="empty-state py-12">
                  <FileText className="empty-state-icon" />
                  <p className="text-sm text-[var(--text-secondary)]">暂无同步记录</p>
                </div>
              ) : (
                <div className="space-y-2">
                  {syncLogs.map(log => (
                    <div key={log.id} className="rounded-[10px] p-3.5 bg-[var(--bg-secondary)] border border-[var(--border-light)]">
                      <div className="flex items-center justify-between mb-1.5">
                        <div className="flex items-center gap-1.5">
                          {log.status === 'success' ? (
                            <CheckCircle className="w-4 h-4 text-[var(--success-600)]" strokeWidth={1.5} />
                          ) : log.status === 'failed' ? (
                            <XCircle className="w-4 h-4 text-[var(--error-600)]" strokeWidth={1.5} />
                          ) : (
                            <Loader2 className="w-4 h-4 text-[var(--primary-500)] animate-spin" strokeWidth={1.5} />
                          )}
                          <span className="text-sm font-medium text-[var(--text-primary)]">
                            {log.status === 'success' ? '成功' : log.status === 'failed' ? '失败' : '进行中'}
                          </span>
                        </div>
                        <span className="font-mono tabular-nums text-xs text-[var(--text-tertiary)]">
                          {formatTime(log.start_time)}
                        </span>
                      </div>
                      <div className="flex items-center gap-4 font-mono tabular-nums text-xs text-[var(--text-secondary)]">
                        <span>耗时 {formatDuration(log.duration_ms)}</span>
                        <span>新增 {log.new_count} 封</span>
                      </div>
                      {log.error && (
                        <div className="mt-2 text-xs font-mono text-[var(--error-600)] bg-[var(--error-50)] rounded-[6px] p-2 break-words">
                          {log.error}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
            <div className="px-6 py-4 border-t border-[var(--border-light)] flex items-center justify-between">
              <span className="text-xs text-[var(--text-tertiary)]">仅显示最近 10 条记录</span>
              <Link
                to={`/sync-logs?account_id=${logsAccount.id}`}
                onClick={() => setLogsAccount(null)}
                className="btn btn-secondary btn-sm"
              >
                <span>查看全部日志</span>
                <ArrowUpRight className="w-3.5 h-3.5" strokeWidth={1.5} />
              </Link>
            </div>
          </div>
        </>
      )}
    </div>
  )
}
