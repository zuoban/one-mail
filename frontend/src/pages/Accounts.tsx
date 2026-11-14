import { useState, useEffect, useRef, useCallback } from 'react'
import axios from 'axios'
import { accountApi, type AccountInput } from '../api'
import { useAccounts } from '../accounts/account-context'
import AccountDrawer, { type AccountFormState } from '../accounts/AccountDrawer'
import type { EmailAccount } from '../api'
import { Plus, Trash2, Mail, Plug, Pencil, Loader2 } from 'lucide-react'
import ConfirmDialog from '../components/ConfirmDialog'
import Tooltip from '../components/Tooltip'

export default function Accounts() {
  const { accounts, providers, loadingAccounts, accountsError, loadAccounts, accountStore } = useAccounts()
  const [loading, setLoading] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [testingAccounts, setTestingAccounts] = useState<Set<number>>(() => new Set())
  const [deleteError, setDeleteError] = useState('')
  const [showModal, setShowModal] = useState(false)
  const [editingAccount, setEditingAccount] = useState<EmailAccount | null>(null)
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<EmailAccount | null>(null)
  const [form, setForm] = useState<AccountFormState>({
    email: '',
    provider: '',
    username: '',
    password: '',
    imap_host: '',
    imap_port: '993',
  })
  const [error, setError] = useState('')
  const [usernameEdited, setUsernameEdited] = useState(false)
  const deletePending = useRef(false)
  const testPending = useRef(new Set<number>())
  const submission = useRef<number | null>(null)
  const formInstance = useRef(0)
  const mounted = useRef(false)
  const lifecycle = useRef(0)
  const toastTimer = useRef<number | undefined>(undefined)
  const drawerRef = useRef<HTMLDivElement>(null)

  const invalidateLifecycle = useCallback(() => {
    mounted.current = false
    lifecycle.current++
    formInstance.current++
    window.clearTimeout(toastTimer.current)
  }, [])

  useEffect(() => {
    mounted.current = true
    lifecycle.current++
    return invalidateLifecycle
  }, [invalidateLifecycle])

  const closeModal = () => {
    if (submission.current !== null) return
    formInstance.current++
    setShowModal(false)
    setEditingAccount(null)
  }

  useEffect(() => {
    if (!showModal) return

    const drawer = drawerRef.current
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    drawer?.focus()

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        if (submission.current === null) {
          formInstance.current++
          setShowModal(false)
          setEditingAccount(null)
        }
        return
      }
      if (event.key !== 'Tab' || !drawer) return

      const controls = drawer.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), select:not([disabled])')
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


  const showToast = (message: string, type: 'success' | 'error') => {
    if (!mounted.current) return
    window.clearTimeout(toastTimer.current)
    setToast({ message, type })
    toastTimer.current = window.setTimeout(() => { if (mounted.current) setToast(null) }, 4000)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (submission.current !== null) return
    const instance = formInstance.current
    submission.current = instance
    const current = () => mounted.current && formInstance.current === instance && submission.current === instance
    setError('')
    setLoading(true)
    try {
      const provider = providers.find(provider => provider.id === form.provider)
      if (!provider) { setError('请先读取并选择邮箱服务商'); return }
      const input: AccountInput = { email: form.email, provider: form.provider, username: form.username, password: form.password }
      if (provider.custom) {
        const port = Number(form.imap_port)
        if (!form.imap_host.trim() || !Number.isInteger(port) || port < 1 || port > 65535) {
          setError('请填写 IMAP 主机和有效端口（1–65535）')
          return
        }
        input.imap_host = form.imap_host.trim()
        input.imap_port = port
      }
      let mutation: { response: { data: EmailAccount; refresh_error?: string }; updated: Promise<boolean> }
      if (editingAccount) {
        const updateData: Partial<AccountInput> = {}
        if (form.email !== editingAccount.email) updateData.email = form.email
        if (form.provider !== editingAccount.provider) updateData.provider = form.provider
        if (form.username !== editingAccount.username) updateData.username = form.username
        if (form.password) updateData.password = form.password
        if (provider.custom) {
          updateData.imap_host = input.imap_host
          updateData.imap_port = input.imap_port
        }
        mutation = await accountStore.update(editingAccount.id, updateData)
      } else {
        mutation = await accountStore.add(input)
      }
      if (!current()) return
      submission.current = null
      setLoading(false)
      closeModal()
      setForm({ email: '', provider: providers[0]?.id || '', username: '', password: '', imap_host: '', imap_port: '993' })
      const updated = await mutation.updated
      if (mounted.current && formInstance.current === instance + 1) {
        const initialSyncFailed = !editingAccount && Boolean(mutation.response.refresh_error)
        const message = initialSyncFailed
          ? `账户已添加，但首次同步失败，可手动刷新${updated ? '' : '；账户列表读取失败，请重试读取列表'}`
          : updated ? (editingAccount ? '账户已更新' : '账户已添加') : '账户已保存，但账户列表读取失败，请重试读取列表'
        showToast(message, updated && !initialSyncFailed ? 'success' : 'error')
      }
    } catch (err: unknown) {
      if (!current()) return
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setError(message || (editingAccount ? '更新失败' : '添加失败'))
    } finally {
      if (current()) {
        submission.current = null
        setLoading(false)
      }
    }
  }

  const openAddModal = () => {
    if (submission.current !== null) return
    formInstance.current++
    setEditingAccount(null)
    setUsernameEdited(false)
    setForm({ email: '', provider: providers[0]?.id || '', username: '', password: '', imap_host: '', imap_port: '993' })
    setError('')
    setShowModal(true)
  }

  const openEditModal = (account: EmailAccount) => {
    if (submission.current !== null) return
    formInstance.current++
    setEditingAccount(account)
    setUsernameEdited(true)
    setForm({
      email: account.email,
      provider: account.provider,
      username: account.username,
      password: '',
      imap_host: account.imap_host || '',
      imap_port: String(account.imap_port || 993),
    })
    setError('')
    setShowModal(true)
  }

  const confirmDeleteAccount = async () => {
    if (!confirmDelete || deletePending.current) return
    const generation = lifecycle.current
    deletePending.current = true
    setDeleting(true)
    setDeleteError('')
    try {
      const mutation = await accountStore.remove(confirmDelete.id)
      if (!mounted.current || lifecycle.current !== generation) return
      setConfirmDelete(null)
      const updated = await mutation.updated
      if (mounted.current && lifecycle.current === generation) showToast(updated ? '账户已删除' : '账户已删除，但账户列表读取失败，请重试读取列表', updated ? 'success' : 'error')
    } catch (err: unknown) {
      if (!mounted.current || lifecycle.current !== generation) return
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setDeleteError(message || '删除失败，请重试')
    } finally {
      if (mounted.current && lifecycle.current === generation) {
        deletePending.current = false
        setDeleting(false)
      }
    }
  }

  const handleTest = async (id: number) => {
    if (testPending.current.has(id)) return
    const generation = lifecycle.current
    testPending.current.add(id)
    setTestingAccounts(new Set(testPending.current))
    try {
      await accountApi.test(id)
      if (mounted.current && lifecycle.current === generation) showToast('连接测试通过', 'success')
    } catch (err: unknown) {
      if (!mounted.current || lifecycle.current !== generation) return
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      showToast(message || '连接失败', 'error')
    } finally {
      if (mounted.current && lifecycle.current === generation) {
        testPending.current.delete(id)
        setTestingAccounts(new Set(testPending.current))
      }
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
        pending={deleting}
        error={deleteError}
        onConfirm={confirmDeleteAccount}
        onCancel={() => { if (!deletePending.current) { setConfirmDelete(null); setDeleteError('') } }}
      />

      {/* Toast */}
      {toast && (
        <div className="fixed left-4 right-4 top-4 z-50 md:left-auto md:right-4">
          <div
            role={toast.type === 'error' ? 'alert' : 'status'}
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
          disabled={loading}
          className="btn btn-primary w-full sm:w-auto"
        >
          <Plus className="w-4 h-4" strokeWidth={1.5} />
          <span>添加账户</span>
        </button>
      </div>

      {loadingAccounts && <p role="status" className="py-3 text-sm text-[var(--text-secondary)]">正在加载账户列表…</p>}
      {accountsError && <div role="alert" className="alert alert-error mb-4 flex flex-wrap items-center gap-2"><span>{accountsError}</span><button type="button" disabled={loadingAccounts} onClick={() => void loadAccounts()} className="btn btn-secondary btn-sm">重试读取账户</button></div>}
      {/* Account List */}
      {accounts.length === 0 ? (!loadingAccounts && !accountsError && (
        <div className="rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] shadow-[var(--shadow-sm)] empty-state">
          <Mail className="empty-state-icon" strokeWidth={1.5} />
          <p className="text-base font-medium text-[var(--text-primary)]">暂无邮箱账户</p>
          <p className="text-sm mt-1 text-[var(--text-tertiary)]">点击右上角添加你的第一个邮箱账户</p>
        </div>
      )) : (
        <div className="rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] shadow-[var(--shadow-sm)] divide-y divide-[var(--border-light)] overflow-hidden">
          {accounts.map(account => (
            <div
              key={account.id}
              className="account-row p-4 md:p-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between hover:bg-[var(--bg-nav-hover)] transition-colors"
            >
              <div className="flex items-center gap-4 min-w-0">
                <div
                  className="w-10 h-10 rounded-lg border border-[var(--border-light)] bg-[var(--bg-secondary)] flex items-center justify-center text-sm font-medium text-[var(--text-secondary)] flex-shrink-0"
                  aria-hidden="true"
                >
                  {account.email[0].toUpperCase()}
                </div>

                <div className="min-w-0">
                  <div className="flex items-center gap-2 min-w-0">
                    <p className="text-sm font-medium text-[var(--text-primary)] truncate">
                      {account.email}
                    </p>
                  </div>

                  <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-[var(--text-tertiary)]">
                    <span>{providers.find(p => p.id === account.provider)?.label || account.provider}</span>
                    <span id={`account-connection-status-${account.id}`} role="status" aria-live="polite" aria-atomic="true" className="inline-flex items-center gap-2">
                      {!testingAccounts.has(account.id) && <span aria-hidden="true" className={`status-dot ${account.status === 'active' ? 'status-dot-success' : 'status-dot-warning'}`} />}
                      <span className={testingAccounts.has(account.id) || account.status === 'active' ? 'text-[var(--text-secondary)]' : ''}>
                        {testingAccounts.has(account.id) ? '正在测试连接…' : account.status === 'active' ? '已连接' : '未连接'}
                      </span>
                    </span>
                    <span className="font-mono tabular-nums">
                      上次刷新: {formatTime(account.last_sync_time)}
                    </span>
                  </div>
                </div>
              </div>

              <div className="flex flex-wrap items-center gap-2 flex-shrink-0">
                <div className="flex items-center gap-1">
                  <Tooltip content={testingAccounts.has(account.id) ? '正在测试连接' : '测试连接'}>
                    <button
                      type="button"
                      onClick={() => handleTest(account.id)}
                      disabled={testingAccounts.has(account.id)}
                      aria-busy={testingAccounts.has(account.id)}
                      aria-describedby={testingAccounts.has(account.id) ? `account-connection-status-${account.id}` : undefined}
                      className="btn btn-ghost w-9 h-9 p-0"
                      aria-label={testingAccounts.has(account.id) ? '正在测试连接' : '测试连接'}
                    >
                      {testingAccounts.has(account.id)
                        ? <Loader2 aria-hidden="true" className="w-4 h-4 animate-spin text-[var(--text-secondary)]" strokeWidth={1.5} />
                        : <Plug aria-hidden="true" className="w-4 h-4 text-[var(--text-tertiary)]" strokeWidth={1.5} />}
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
                      onClick={() => { if (!deletePending.current) { setDeleteError(''); setConfirmDelete(account) } }}
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
        <AccountDrawer drawerRef={drawerRef} editingAccount={editingAccount} loading={loading} form={form} setForm={setForm}
          usernameEdited={usernameEdited} setUsernameEdited={setUsernameEdited} error={error} closeModal={closeModal} handleSubmit={handleSubmit} />
      )}

    </div>
  )
}
