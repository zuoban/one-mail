import { useState, useEffect } from 'react'
import { authApi, telegramApi } from '../api'
import useAuth from '../context/useAuth'
import { User, Lock, Save, Send } from 'lucide-react'
import axios from 'axios'
import type { TelegramConfig } from '../api'

const settingsTabs = [
  { key: 'profile', label: '个人信息', icon: User },
  { key: 'password', label: '修改密码', icon: Lock },
  { key: 'telegram', label: 'Telegram 通知', icon: Send },
] as const

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

  useEffect(() => {
    if (user) {
      setProfileForm({
        username: user.username || '',
        email: user.email || '',
      })
    }
    loadTelegramConfig()
  }, [user])

  const showToast = (message: string, type: 'success' | 'error') => {
    setToast({ message, type })
    window.setTimeout(() => setToast(null), 2000)
  }

  const loadTelegramConfig = async () => {
    try {
      const res = await telegramApi.getConfig()
      setTelegramConfig(res.data)
    } catch (err) {
      console.error('Failed to load telegram config:', err)
    }
  }

  const handleProfileSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setProfileError('')
    setLoading(true)
    try {
      const res = await authApi.updateProfile({
        username: profileForm.username,
        email: profileForm.email,
      })
      updateUser(res.data)
      showToast('个人信息已保存', 'success')
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setProfileError(message || '更新失败')
    } finally {
      setLoading(false)
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
      setPasswordError('新密码至少需要 6 个字符')
      return
    }

    setLoading(true)
    try {
      await authApi.changePassword({
        old_password: passwordForm.old_password,
        new_password: passwordForm.new_password,
      })
      showToast('密码已更新', 'success')
      setPasswordForm({
        old_password: '',
        new_password: '',
        confirm_password: '',
      })
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setPasswordError(message || '修改失败')
    } finally {
      setLoading(false)
    }
  }

  const handleTelegramSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setTelegramError('')
    setTelegramLoading(true)
    try {
      const res = await telegramApi.updateConfig({
        enabled: telegramConfig.enabled,
        bot_token: telegramConfig.bot_token,
        chat_id: telegramConfig.chat_id,
      })
      setTelegramConfig(res.data)
      showToast('Telegram 配置已保存', 'success')
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setTelegramError(message || '保存失败')
    } finally {
      setTelegramLoading(false)
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
      showToast('测试消息已发送', 'success')
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setTelegramError(message || '发送测试消息失败')
    } finally {
      setTelegramTesting(false)
    }
  }

  return (
    <div className="max-w-3xl mx-auto p-4 md:p-8">
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
      <div className="mb-6">
        <h1 className="text-2xl font-medium tracking-[-0.015em] text-[var(--text-primary)]">设置</h1>
        <p className="mt-1 text-sm text-[var(--text-secondary)]">
          管理账号资料、登录密码与 Telegram 邮件推送
        </p>
      </div>

      {/* Segmented Tabs */}
      <div className="mb-6 segmented flex sm:inline-flex w-full sm:w-auto">
        {settingsTabs.map((tab) => {
          const Icon = tab.icon
          const isActive = activeTab === tab.key
          return (
            <button
              key={tab.key}
              type="button"
              onClick={() => setActiveTab(tab.key)}
              aria-pressed={isActive}
              className={`segmented-item flex-1 sm:flex-initial h-8 px-2.5 sm:px-3.5 text-xs sm:text-sm gap-1.5 sm:gap-2 ${isActive ? 'segmented-item-active' : ''}`}
            >
              <Icon className={`w-3.5 h-3.5 sm:w-4 sm:h-4 flex-shrink-0 ${isActive ? 'text-[var(--primary-500)]' : 'text-[var(--text-tertiary)]'}`} strokeWidth={1.5} />
              <span className="truncate">{tab.label}</span>
            </button>
          )
        })}
      </div>

      {/* Profile Panel */}
      {activeTab === 'profile' && (
        <section className="rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] p-5 md:p-6 shadow-[var(--shadow-sm)]">
          <div className="pb-4 mb-5 border-b border-[var(--border-light)]">
            <h2 className="text-base font-medium text-[var(--text-primary)]">个人信息</h2>
            <p className="mt-0.5 text-sm text-[var(--text-secondary)]">更新用户名和联系邮箱</p>
          </div>

          {profileError && (
            <div className="alert alert-error mb-4">{profileError}</div>
          )}

          <form onSubmit={handleProfileSubmit} className="space-y-4">
            <div className="grid gap-4 md:grid-cols-2">
              <div>
                <label className="block text-sm font-medium text-[var(--text-secondary)] mb-1.5">
                  用户名
                </label>
                <input
                  type="text"
                  value={profileForm.username}
                  onChange={(e) => setProfileForm({ ...profileForm, username: e.target.value })}
                  placeholder="请输入用户名"
                  minLength={3}
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-[var(--text-secondary)] mb-1.5">
                  联系邮箱
                </label>
                <input
                  type="email"
                  value={profileForm.email}
                  onChange={(e) => setProfileForm({ ...profileForm, email: e.target.value })}
                  placeholder="name@example.com"
                />
              </div>
            </div>

            <div className="flex justify-end pt-2">
              <button
                type="submit"
                disabled={loading}
                className="btn btn-primary w-full sm:w-auto"
              >
                <Save className="w-4 h-4" strokeWidth={1.5} />
                <span>{loading ? '保存中' : '保存修改'}</span>
              </button>
            </div>
          </form>
        </section>
      )}

      {/* Password Panel */}
      {activeTab === 'password' && (
        <section className="rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] p-5 md:p-6 shadow-[var(--shadow-sm)]">
          <div className="pb-4 mb-5 border-b border-[var(--border-light)]">
            <h2 className="text-base font-medium text-[var(--text-primary)]">修改密码</h2>
            <p className="mt-0.5 text-sm text-[var(--text-secondary)]">更新当前账号的登录密码</p>
          </div>

          {passwordError && (
            <div className="alert alert-error mb-4">{passwordError}</div>
          )}

          <form onSubmit={handlePasswordSubmit} className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-[var(--text-secondary)] mb-1.5">
                原密码
              </label>
              <input
                type="password"
                value={passwordForm.old_password}
                onChange={(e) => setPasswordForm({ ...passwordForm, old_password: e.target.value })}
                placeholder="输入当前密码"
                required
              />
            </div>

            <div className="grid gap-4 md:grid-cols-2">
              <div>
                <label className="block text-sm font-medium text-[var(--text-secondary)] mb-1.5">
                  新密码
                </label>
                <input
                  type="password"
                  value={passwordForm.new_password}
                  onChange={(e) => setPasswordForm({ ...passwordForm, new_password: e.target.value })}
                  placeholder="至少 6 个字符"
                  minLength={6}
                  required
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-[var(--text-secondary)] mb-1.5">
                  确认新密码
                </label>
                <input
                  type="password"
                  value={passwordForm.confirm_password}
                  onChange={(e) => setPasswordForm({ ...passwordForm, confirm_password: e.target.value })}
                  placeholder="再次输入新密码"
                  required
                />
              </div>
            </div>

            <div className="flex justify-end pt-2">
              <button
                type="submit"
                disabled={loading}
                className="btn btn-primary w-full sm:w-auto"
              >
                <Lock className="w-4 h-4" strokeWidth={1.5} />
                <span>{loading ? '修改中' : '更新密码'}</span>
              </button>
            </div>
          </form>
        </section>
      )}

      {/* Telegram Panel */}
      {activeTab === 'telegram' && (
        <section className="rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] p-5 md:p-6 shadow-[var(--shadow-sm)]">
          <div className="pb-4 mb-5 border-b border-[var(--border-light)]">
            <h2 className="text-base font-medium text-[var(--text-primary)]">Telegram 通知</h2>
            <p className="mt-0.5 text-sm text-[var(--text-secondary)]">连接 Bot 后可将新邮件推送到指定会话</p>
          </div>

          {telegramError && (
            <div className="alert alert-error mb-4">{telegramError}</div>
          )}

          <form onSubmit={handleTelegramSubmit} className="space-y-4">
            <div className="flex items-center justify-between gap-4 p-4 rounded-[10px] bg-[var(--bg-secondary)] border border-[var(--border-light)]">
              <div>
                <span className="block text-sm font-medium text-[var(--text-primary)]">启用自动推送</span>
                <span className="block text-xs text-[var(--text-tertiary)] mt-0.5">
                  收到新邮件时自动转发摘要到 Telegram
                </span>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={telegramConfig.enabled}
                onClick={() => setTelegramConfig({ ...telegramConfig, enabled: !telegramConfig.enabled })}
                className={`relative h-6 w-11 rounded-full transition-colors cursor-pointer ${
                  telegramConfig.enabled ? 'bg-[var(--primary-500)]' : 'bg-[var(--bg-tertiary)] shadow-[inset_0_0_0_1px_var(--border-default)]'
                }`}
              >
                <span
                  className={`toggle-thumb absolute left-0.5 top-0.5 h-5 w-5 rounded-full transition-transform ${
                    telegramConfig.enabled ? 'translate-x-5' : 'translate-x-0'
                  }`}
                />
              </button>
            </div>

            <div>
              <label className="block text-sm font-medium text-[var(--text-secondary)] mb-1.5">
                Bot Token
              </label>
              <input
                type="text"
                value={telegramConfig.bot_token}
                onChange={(e) => setTelegramConfig({ ...telegramConfig, bot_token: e.target.value })}
                className="font-mono text-sm"
                placeholder="123456789:ABCdefGHIjklMNOpqrsTUVwxyz"
              />
              <p className="mt-1.5 text-xs text-[var(--text-tertiary)]">从 @BotFather 创建机器人后获取</p>
            </div>

            <div>
              <label className="block text-sm font-medium text-[var(--text-secondary)] mb-1.5">
                Chat ID
              </label>
              <input
                type="text"
                value={telegramConfig.chat_id}
                onChange={(e) => setTelegramConfig({ ...telegramConfig, chat_id: e.target.value })}
                className="font-mono text-sm"
                placeholder="-1001234567890"
              />
              <p className="mt-1.5 text-xs text-[var(--text-tertiary)]">支持个人用户 ID、群组 ID 或频道 ID</p>
            </div>

            <div className="flex flex-col-reverse gap-2 pt-2 sm:flex-row sm:justify-between">
              <button
                type="button"
                onClick={handleTelegramTest}
                disabled={telegramTesting || !telegramConfig.bot_token || !telegramConfig.chat_id}
                className="btn btn-secondary w-full sm:w-auto"
              >
                {telegramTesting ? '发送中' : '发送测试消息'}
              </button>
              <button
                type="submit"
                disabled={telegramLoading}
                className="btn btn-primary w-full sm:w-auto"
              >
                <Save className="w-4 h-4" strokeWidth={1.5} />
                <span>{telegramLoading ? '保存中' : '保存配置'}</span>
              </button>
            </div>
          </form>
        </section>
      )}
    </div>
  )
}
