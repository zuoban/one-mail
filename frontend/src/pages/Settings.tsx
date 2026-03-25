import { useState, useEffect } from 'react'
import { authApi, telegramApi } from '../api'
import useAuth from '../context/useAuth'
import { User, Lock, Save, Key, Send, Bell } from 'lucide-react'
import axios from 'axios'
import type { TelegramConfig } from '../api'

const settingsTabs = [
  { key: 'profile', label: '个人信息', icon: User, description: '管理用户名和联系邮箱' },
  { key: 'password', label: '修改密码', icon: Lock, description: '更新登录凭据与账号安全' },
  { key: 'telegram', label: 'Telegram 通知', icon: Send, description: '把新邮件推送到 Telegram' },
] as const

const panelClass = 'rounded-[24px] border border-[var(--border-light)] bg-[var(--bg-primary)] p-4 shadow-[var(--shadow-md)] md:p-6'
const fieldCardClass = 'rounded-2xl border border-[var(--border-light)] bg-[var(--bg-secondary)] p-4'
const inputClass = 'w-full rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] px-4 py-2.5 text-[var(--text-primary)] focus:outline-none focus:ring-2 focus:ring-[var(--primary-500)]'

export default function Settings() {
  const { user, updateUser } = useAuth()
  const [loading, setLoading] = useState(false)
  const [activeTab, setActiveTab] = useState<'profile' | 'password' | 'telegram'>('profile')
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null)

  const [profileForm, setProfileForm] = useState({
    username: '',
    email: '',
  })
  const [profileError, setProfileError] = useState('')

  const [passwordForm, setPasswordForm] = useState({
    old_password: '',
    new_password: '',
    confirm_password: '',
  })
  const [passwordError, setPasswordError] = useState('')

  const [telegramConfig, setTelegramConfig] = useState<TelegramConfig>({
    enabled: false,
    bot_token: '',
    chat_id: '',
  })
  const [telegramLoading, setTelegramLoading] = useState(false)
  const [telegramTesting, setTelegramTesting] = useState(false)
  const [telegramError, setTelegramError] = useState('')
  const activeTabMeta = settingsTabs.find((tab) => tab.key === activeTab) ?? settingsTabs[0]

  useEffect(() => {
    if (user) {
      setProfileForm({
        username: user.username,
        email: user.email,
      })
    }
    loadTelegramConfig()
  }, [user])

  const loadTelegramConfig = async () => {
    try {
      const res = await telegramApi.getConfig()
      setTelegramConfig(res.data)
    } catch (e) {
      console.error('Failed to load telegram config', e)
    }
  }

  const handleProfileSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setProfileError('')
    setLoading(true)

    try {
      const res = await authApi.updateProfile(profileForm)
      updateUser(res.data)
      setToast({ message: '个人信息更新成功', type: 'success' })
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setProfileError(message || '更新失败')
    } finally {
      setLoading(false)
      setTimeout(() => setToast(null), 2000)
    }
  }

  const handlePasswordSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setPasswordError('')

    if (passwordForm.new_password !== passwordForm.confirm_password) {
      setPasswordError('两次输入的新密码不一致')
      return
    }

    if (passwordForm.new_password.length < 6) {
      setPasswordError('新密码至少需要6个字符')
      return
    }

    setLoading(true)

    try {
      await authApi.changePassword({
        old_password: passwordForm.old_password,
        new_password: passwordForm.new_password,
      })
      setToast({ message: '密码修改成功', type: 'success' })
      setPasswordForm({ old_password: '', new_password: '', confirm_password: '' })
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setPasswordError(message || '密码修改失败')
    } finally {
      setLoading(false)
      setTimeout(() => setToast(null), 2000)
    }
  }

  const handleTelegramSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setTelegramError('')
    setTelegramLoading(true)

    try {
      await telegramApi.updateConfig({
        enabled: telegramConfig.enabled,
        bot_token: telegramConfig.bot_token,
        chat_id: telegramConfig.chat_id,
      })
      setToast({ message: 'Telegram 配置已保存', type: 'success' })
      loadTelegramConfig()
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setTelegramError(message || '保存失败')
    } finally {
      setTelegramLoading(false)
      setTimeout(() => setToast(null), 2000)
    }
  }

  const handleTelegramTest = async () => {
    setTelegramError('')
    setTelegramTesting(true)

    try {
      await telegramApi.testConnection({
        bot_token: telegramConfig.bot_token,
        chat_id: telegramConfig.chat_id,
      })
      setToast({ message: '测试消息发送成功', type: 'success' })
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setTelegramError(message || '测试失败')
    } finally {
      setTelegramTesting(false)
      setTimeout(() => setToast(null), 2000)
    }
  }

  return (
    <div className="h-full overflow-auto">
      <div className="max-w-4xl mx-auto p-4 md:p-8">
        <div className="mb-6 rounded-[28px] border border-[var(--border-light)] bg-[var(--bg-primary)]/85 p-5 shadow-[var(--shadow-lg)] backdrop-blur md:p-7">
          <div className="min-w-0">
            <div className="mb-3 inline-flex items-center gap-2 rounded-full border border-[var(--primary-200)] bg-[var(--bg-accent-soft)] px-3 py-1 text-xs font-medium text-[var(--primary-700)]">
              <span className="status-dot status-dot-success" />
              账户与通知中心
            </div>
            <h1 className="text-3xl md:text-4xl font-semibold tracking-tight text-[var(--text-primary)]">设置</h1>
            <p className="mt-2 max-w-2xl text-sm md:text-base text-[var(--text-secondary)]">
              在这里统一管理账号资料、登录安全和 Telegram 推送，让 One-Mail 更贴合你的工作节奏。
            </p>
          </div>
        </div>

        {toast && (
          <div
            className={`fixed left-4 right-4 top-4 px-4 py-3 rounded-lg shadow-lg border z-50 md:left-auto md:right-4 ${
              toast.type === 'success'
                ? 'bg-[var(--success-50)] text-[var(--success-700)] border-[var(--success-100)]'
                : 'bg-[var(--error-50)] text-[var(--error-700)] border-[var(--error-100)]'
            }`}
          >
            {toast.message}
          </div>
        )}

        <div className="mb-6 rounded-2xl border border-[var(--border-light)] bg-[var(--bg-primary)] p-2 shadow-sm">
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
            {settingsTabs.map((tab) => {
              const Icon = tab.icon
              const isActive = activeTab === tab.key

              return (
                <button
                  key={tab.key}
                  type="button"
                  onClick={() => setActiveTab(tab.key)}
                  className={`${
                    tab.key === 'telegram' ? 'col-span-2 sm:col-span-1' : ''
                  } flex min-h-[56px] items-center justify-center gap-2 rounded-xl border px-4 py-3 text-sm font-medium transition-all ${
                    isActive
                      ? 'border-[var(--primary-500)] bg-gradient-to-br from-[var(--primary-500)] to-[var(--primary-600)] text-white shadow-lg shadow-[var(--primary-500)]/20'
                      : 'border-transparent bg-[var(--bg-secondary)] text-[var(--text-secondary)] hover:border-[var(--border-light)] hover:bg-[var(--bg-tertiary)] hover:text-[var(--text-primary)]'
                  }`}
                  aria-pressed={isActive}
                >
                  <Icon className={`w-4 h-4 ${isActive ? 'opacity-100' : 'opacity-80'}`} />
                  <span className="truncate">{tab.label}</span>
                </button>
              )
            })}
          </div>
        </div>

        <div className="mb-6 flex items-start gap-3 rounded-2xl border border-[var(--border-light)] bg-[var(--bg-secondary)]/85 px-4 py-4 backdrop-blur">
          <div className="mt-0.5 flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl bg-[var(--bg-primary)] text-[var(--primary-600)] shadow-sm">
            <activeTabMeta.icon className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <div className="text-sm font-semibold text-[var(--text-primary)]">{activeTabMeta.label}</div>
            <p className="mt-1 text-sm text-[var(--text-secondary)]">{activeTabMeta.description}</p>
          </div>
        </div>

        {activeTab === 'profile' && (
          <div className={panelClass}>
            <div className="mb-6 flex flex-col gap-2 border-b border-[var(--border-light)] pb-4">
              <h2 className="flex items-center gap-2 text-lg font-medium text-[var(--text-primary)]">
                <User className="w-5 h-5" />
                个人信息
              </h2>
              <p className="text-sm text-[var(--text-secondary)]">
                这些信息会用于账号识别、通知展示和后续找回流程。
              </p>
            </div>

            {profileError && (
              <div className="alert alert-error mb-4 rounded-lg">
                {profileError}
              </div>
            )}

            <form onSubmit={handleProfileSubmit} className="space-y-4">
              <div className="grid gap-4 md:grid-cols-2">
                <div className={fieldCardClass}>
                  <label className="mb-2 block text-sm font-medium text-[var(--text-secondary)]">
                    用户名
                  </label>
                  <input
                    type="text"
                    value={profileForm.username}
                    onChange={(e) => setProfileForm({ ...profileForm, username: e.target.value })}
                    className={inputClass}
                    placeholder="请输入用户名"
                    minLength={3}
                  />
                </div>

                <div className={fieldCardClass}>
                  <label className="mb-2 block text-sm font-medium text-[var(--text-secondary)]">
                    邮箱
                  </label>
                  <input
                    type="email"
                    value={profileForm.email}
                    onChange={(e) => setProfileForm({ ...profileForm, email: e.target.value })}
                    className={inputClass}
                    placeholder="请输入邮箱"
                  />
                </div>
              </div>

              <div className="flex justify-end pt-4">
                <button
                  type="submit"
                  disabled={loading}
                  className="btn btn-primary flex w-full sm:w-auto items-center justify-center gap-2"
                >
                  <Save className="w-4 h-4" />
                  {loading ? '保存中...' : '保存修改'}
                </button>
              </div>
            </form>
          </div>
        )}

        {activeTab === 'password' && (
          <div className={panelClass}>
            <div className="mb-6 flex flex-col gap-2 border-b border-[var(--border-light)] pb-4">
              <h2 className="flex items-center gap-2 text-lg font-medium text-[var(--text-primary)]">
                <Key className="w-5 h-5" />
                修改密码
              </h2>
              <p className="text-sm text-[var(--text-secondary)]">
                建议使用更长的组合密码，并定期更新以保持账号安全。
              </p>
            </div>

            {passwordError && (
              <div className="alert alert-error mb-4 rounded-lg">
                {passwordError}
              </div>
            )}

            <form onSubmit={handlePasswordSubmit} className="space-y-4">
              <div className={fieldCardClass}>
                <label className="mb-2 block text-sm font-medium text-[var(--text-secondary)]">
                  原密码
                </label>
                <input
                  type="password"
                  value={passwordForm.old_password}
                  onChange={(e) => setPasswordForm({ ...passwordForm, old_password: e.target.value })}
                  className={inputClass}
                  placeholder="请输入原密码"
                  required
                />
              </div>

              <div className="grid gap-4 md:grid-cols-2">
                <div className={fieldCardClass}>
                  <label className="mb-2 block text-sm font-medium text-[var(--text-secondary)]">
                    新密码
                  </label>
                  <input
                    type="password"
                    value={passwordForm.new_password}
                    onChange={(e) => setPasswordForm({ ...passwordForm, new_password: e.target.value })}
                    className={inputClass}
                    placeholder="请输入新密码（至少6个字符）"
                    minLength={6}
                    required
                  />
                </div>

                <div className={fieldCardClass}>
                  <label className="mb-2 block text-sm font-medium text-[var(--text-secondary)]">
                    确认新密码
                  </label>
                  <input
                    type="password"
                    value={passwordForm.confirm_password}
                    onChange={(e) => setPasswordForm({ ...passwordForm, confirm_password: e.target.value })}
                    className={inputClass}
                    placeholder="请再次输入新密码"
                    required
                  />
                </div>
              </div>

              <div className="flex justify-end pt-4">
                <button
                  type="submit"
                  disabled={loading}
                  className="btn btn-primary flex w-full sm:w-auto items-center justify-center gap-2"
                >
                  <Lock className="w-4 h-4" />
                  {loading ? '修改中...' : '修改密码'}
                </button>
              </div>
            </form>
          </div>
        )}

        {activeTab === 'telegram' && (
          <div className={panelClass}>
            <div className="mb-6 flex flex-col gap-2 border-b border-[var(--border-light)] pb-4">
              <h2 className="flex items-center gap-2 text-lg font-medium text-[var(--text-primary)]">
                <Bell className="w-5 h-5" />
                Telegram 通知
              </h2>
              <p className="text-sm text-[var(--text-secondary)]">
                连接 Bot 后，你可以在 Telegram 第一时间收到新邮件提醒与重要消息转发。
              </p>
            </div>

            {telegramError && (
              <div className="alert alert-error mb-4 rounded-lg">
                {telegramError}
              </div>
            )}

            <form onSubmit={handleTelegramSubmit} className="space-y-4">
              <div className={fieldCardClass}>
                <div className="flex items-center justify-between gap-4">
                  <div>
                    <label className="block text-sm font-medium text-[var(--text-secondary)]">
                      启用通知
                    </label>
                    <p className="mt-0.5 text-xs text-[var(--text-tertiary)]">
                      开启后新邮件将自动转发到 Telegram
                    </p>
                  </div>
                  <button
                    type="button"
                    onClick={() => setTelegramConfig({ ...telegramConfig, enabled: !telegramConfig.enabled })}
                    className={`relative h-6 w-11 rounded-full transition-colors ${
                      telegramConfig.enabled ? 'bg-[var(--primary-500)]' : 'bg-[var(--bg-tertiary)]'
                    }`}
                  >
                    <span
                      className={`toggle-thumb absolute left-0.5 top-0.5 h-5 w-5 rounded-full transition-transform ${
                        telegramConfig.enabled ? 'translate-x-5' : 'translate-x-0'
                      }`}
                    />
                  </button>
                </div>
              </div>

              <div className="grid gap-4">
                <div className={fieldCardClass}>
                  <label className="mb-2 block text-sm font-medium text-[var(--text-secondary)]">
                    Bot Token
                  </label>
                  <input
                    type="text"
                    value={telegramConfig.bot_token}
                    onChange={(e) => setTelegramConfig({ ...telegramConfig, bot_token: e.target.value })}
                    className={`${inputClass} font-mono text-sm`}
                    placeholder="123456789:ABCdefGHIjklMNOpqrsTUVwxyz"
                  />
                  <p className="mt-2 text-xs text-[var(--text-tertiary)]">
                    从 @BotFather 获取
                  </p>
                </div>

                <div className={fieldCardClass}>
                  <label className="mb-2 block text-sm font-medium text-[var(--text-secondary)]">
                    Chat ID
                  </label>
                  <input
                    type="text"
                    value={telegramConfig.chat_id}
                    onChange={(e) => setTelegramConfig({ ...telegramConfig, chat_id: e.target.value })}
                    className={inputClass}
                    placeholder="-1001234567890"
                  />
                  <p className="mt-2 text-xs text-[var(--text-tertiary)]">
                    可以是用户 ID、群组 ID 或频道 ID
                  </p>
                </div>
              </div>

              <div className="flex flex-col-reverse gap-3 pt-4 sm:flex-row sm:justify-between">
                <button
                  type="button"
                  onClick={handleTelegramTest}
                  disabled={telegramTesting || !telegramConfig.bot_token || !telegramConfig.chat_id}
                  className="w-full rounded-lg border border-[var(--primary-200)] px-4 py-2 text-sm text-[var(--primary-700)] transition-colors hover:bg-[var(--bg-accent-soft)] disabled:cursor-not-allowed disabled:opacity-50 sm:w-auto"
                >
                  {telegramTesting ? '测试中...' : '发送测试消息'}
                </button>
                <button
                  type="submit"
                  disabled={telegramLoading}
                  className="btn btn-primary flex w-full sm:w-auto items-center justify-center gap-2"
                >
                  <Save className="w-4 h-4" />
                  {telegramLoading ? '保存中...' : '保存配置'}
                </button>
              </div>
            </form>
          </div>
        )}
      </div>
    </div>
  )
}
