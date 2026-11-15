import { useEffect, useRef, type Dispatch, type FormEvent, type RefObject, type SetStateAction } from 'react'
import { X } from 'lucide-react'
import type { EmailAccount, EmailProvider } from '../api'
import { useAccounts } from './account-context'

export interface AccountFormState {
  email: string
  password: string
  imap_host: string
  imap_port: string
}


interface Props {
  drawerRef: RefObject<HTMLDivElement | null>
  editingAccount: EmailAccount | null
  loading: boolean
  form: AccountFormState
  setForm: Dispatch<SetStateAction<AccountFormState>>
  provider: EmailProvider | undefined
  error: string
  closeModal: () => void
  handleSubmit: (event: FormEvent) => Promise<void>
}

export default function AccountDrawer({ drawerRef, editingAccount, loading, form, setForm, provider, error, closeModal, handleSubmit }: Props) {
  const { loadingProviders, providersError, accountStore } = useAccounts()
  const isICloud = provider?.id === 'icloud'
  const passwordHelpIds = [editingAccount && 'account-password-help', isICloud && 'account-icloud-password-help'].filter(Boolean).join(' ') || undefined
  const errorRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (error) errorRef.current?.scrollIntoView({ block: 'nearest' })
  }, [error])
  return (
        <>
          <div
            className="overlay-backdrop fixed inset-0 z-40"
            onClick={closeModal}
          />
          <div
            ref={drawerRef}
            role="dialog"
            aria-modal="true"
            aria-labelledby="account-drawer-title"
            aria-describedby="account-drawer-description"
            tabIndex={-1}
            className="account-drawer fixed inset-y-0 right-0 w-full sm:w-[480px] bg-[var(--bg-primary)] border-l border-[var(--border-default)] shadow-[var(--shadow-xl)] z-50 flex flex-col"
          >
            <div className="account-drawer-header border-b border-[var(--border-light)] flex items-center justify-between gap-4">
              <div className="min-w-0">
                <h2 id="account-drawer-title" className="text-base font-medium text-[var(--text-primary)]">
                  {editingAccount ? '编辑邮箱账户' : '添加邮箱账户'}
                </h2>
                <p id="account-drawer-description" className="text-xs text-[var(--text-tertiary)] truncate">
                  {editingAccount ? editingAccount.email : '输入邮箱地址，自动匹配邮箱配置'}
                </p>
              </div>
              <button
                type="button"
                onClick={closeModal}
                disabled={loading}
                className="btn btn-ghost w-9 h-9 p-0"
                aria-label="关闭"
              >
                <X className="w-4 h-4" strokeWidth={1.5} />
              </button>
            </div>

            <form onSubmit={handleSubmit} aria-busy={loading} className="min-h-0 flex-1 overflow-auto flex flex-col">
              <fieldset disabled={loading} className="account-drawer-fields min-w-0 min-h-0 flex-1 overflow-auto p-6 space-y-6 thin-scrollbar">
                <div>
                  <label htmlFor="account-email" className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                    邮箱地址
                  </label>
                  <input
                    id="account-email"
                    type="email"
                    autoComplete="email"
                    autoCapitalize="none"
                    spellCheck={false}
                    aria-describedby="account-provider-status"
                    value={form.email}
                    onChange={event => setForm({ ...form, email: event.target.value })}
                    placeholder="name@example.com"
                    required
                  />
                  <p id="account-provider-status" role="status" aria-live="polite" aria-atomic="true" className="mt-2 text-sm text-[var(--text-secondary)]">
                    {loadingProviders ? '正在读取邮箱配置…'
                      : providersError ? '邮箱配置暂不可用，请重试。'
                      : provider?.custom ? '此邮箱需手动填写 IMAP 服务器信息，使用 TLS 加密连接。'
                      : provider ? `已匹配 ${provider.label}，将自动配置收信服务器。`
                      : '填写完整邮箱地址后，自动识别服务商。'}
                  </p>
                  {providersError && <div role="alert" className="mt-2 text-sm text-[var(--error-700)]">{providersError}<button type="button" disabled={loadingProviders} onClick={() => void accountStore.retryProviders()} className="btn btn-secondary btn-sm ml-2">重试读取服务商</button></div>}
                </div>

                {provider?.custom && (
                  <div className="grid grid-cols-1 sm:grid-cols-[1fr_112px] gap-4">
                    <div className="min-w-0">
                      <label htmlFor="account-imap-host" className="block text-sm font-medium text-[var(--text-secondary)] mb-2">IMAP 主机</label>
                      <input id="account-imap-host" type="text" value={form.imap_host} onChange={event => setForm({ ...form, imap_host: event.target.value })} placeholder="imap.example.com" autoCapitalize="none" spellCheck={false} required />
                    </div>
                    <div className="min-w-0">
                      <label htmlFor="account-imap-port" className="block text-sm font-medium text-[var(--text-secondary)] mb-2">IMAP 端口</label>
                      <input id="account-imap-port" type="number" inputMode="numeric" min={1} max={65535} step={1} value={form.imap_port} onChange={event => setForm({ ...form, imap_port: event.target.value })} required />
                    </div>
                  </div>
                )}

                <div>
                  <label htmlFor="account-password" className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                    {isICloud ? 'App 专用密码' : '密码 / 授权码'}
                    {editingAccount && (
                      <span id="account-password-help" className="text-xs text-[var(--text-tertiary)] font-normal ml-2">留空则保持不变</span>
                    )}
                  </label>
                  <input
                    id="account-password"
                    aria-describedby={passwordHelpIds}
                    type="password"
                    value={form.password}
                    onChange={e => setForm({ ...form, password: e.target.value })}
                    placeholder={editingAccount ? '留空保持原密码' : isICloud ? '输入 Apple 生成的 App 专用密码' : '输入应用专用密码或授权码'}
                    required={!editingAccount}
                  />
                  {isICloud && (
                    <p id="account-icloud-password-help" className="mt-2 text-sm text-[var(--text-secondary)]">
                      先开启 Apple 账户双重认证，再前往{' '}
                      <a href="https://account.apple.com/" target="_blank" rel="noopener noreferrer" className="underline underline-offset-2">Apple 账户（新窗口）</a>
                      {' '}的「登录与安全」生成 App 专用密码，并在此填写。
                    </p>
                  )}
                </div>

                {error && (
                  <div ref={errorRef} role="alert" className="alert alert-error flex items-center gap-2">
                    <X className="w-4 h-4 flex-shrink-0" strokeWidth={1.5} />
                    <span>{error}</span>
                  </div>
                )}
              </fieldset>

              <div className="account-drawer-footer flex flex-col-reverse sm:flex-row sm:justify-end gap-2 px-6 pt-4 border-t border-[var(--border-light)]">
                <button
                  type="button"
                  onClick={closeModal}
                  disabled={loading}
                  className="btn btn-secondary w-full sm:w-auto"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={loading || loadingProviders || Boolean(providersError) || !provider}
                  className="btn btn-primary w-full sm:w-auto"
                >
                  {loading ? '处理中' : editingAccount ? '保存修改' : '添加账户'}
                </button>
              </div>
            </form>
          </div>
        </>
  )
}
