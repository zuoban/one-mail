import { useState, useEffect } from 'react'
import { authApi, telegramApi } from '../api'
import useAuth from '../context/useAuth'
import { User, Lock, Save, Key, Send, Bell } from 'lucide-react'
import axios from 'axios'
import type { TelegramConfig } from '../api'

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
      <div className="max-w-4xl mx-auto p-8">
        <h1 className="text-2xl font-semibold text-[var(--text-primary)] mb-8">设置</h1>

        {toast && (
          <div
            className={`fixed top-4 right-4 px-4 py-3 rounded-lg shadow-lg border z-50 ${
              toast.type === 'success'
                ? 'bg-[var(--success-50)] text-[var(--success-700)] border-[var(--success-100)]'
                : 'bg-[var(--error-50)] text-[var(--error-700)] border-[var(--error-100)]'
            }`}
          >
            {toast.message}
          </div>
        )}

        <div className="flex gap-2 mb-6">
          <button
            onClick={() => setActiveTab('profile')}
            className={`flex items-center gap-2 px-4 py-2 rounded-lg transition-colors ${
              activeTab === 'profile'
                ? 'bg-[var(--primary-600)] text-white'
                : 'bg-[var(--bg-primary)] text-[var(--text-secondary)] hover:bg-[var(--bg-tertiary)]'
            }`}
          >
            <User className="w-4 h-4" />
            个人信息
          </button>
          <button
            onClick={() => setActiveTab('password')}
            className={`flex items-center gap-2 px-4 py-2 rounded-lg transition-colors ${
              activeTab === 'password'
                ? 'bg-[var(--primary-600)] text-white'
                : 'bg-[var(--bg-primary)] text-[var(--text-secondary)] hover:bg-[var(--bg-tertiary)]'
            }`}
          >
            <Lock className="w-4 h-4" />
            修改密码
          </button>
          <button
            onClick={() => setActiveTab('telegram')}
            className={`flex items-center gap-2 px-4 py-2 rounded-lg transition-colors ${
              activeTab === 'telegram'
                ? 'bg-[var(--primary-600)] text-white'
                : 'bg-[var(--bg-primary)] text-[var(--text-secondary)] hover:bg-[var(--bg-tertiary)]'
            }`}
          >
            <Send className="w-4 h-4" />
            Telegram 通知
          </button>
        </div>

        {activeTab === 'profile' && (
          <div className="bg-[var(--bg-primary)] rounded-lg p-6 border border-[var(--border-light)]">
            <h2 className="text-lg font-medium text-[var(--text-primary)] mb-4 flex items-center gap-2">
              <User className="w-5 h-5" />
              个人信息
            </h2>

            {profileError && (
              <div className="alert alert-error mb-4 rounded-lg">
                {profileError}
              </div>
            )}

            <form onSubmit={handleProfileSubmit} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                  用户名
                </label>
                <input
                  type="text"
                  value={profileForm.username}
                  onChange={(e) => setProfileForm({ ...profileForm, username: e.target.value })}
                  className="w-full px-4 py-2 border border-[var(--border-light)] rounded-lg focus:outline-none focus:ring-2 focus:ring-[var(--primary-500)] bg-[var(--bg-secondary)] text-[var(--text-primary)]"
                  placeholder="请输入用户名"
                  minLength={3}
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                  邮箱
                </label>
                <input
                  type="email"
                  value={profileForm.email}
                  onChange={(e) => setProfileForm({ ...profileForm, email: e.target.value })}
                  className="w-full px-4 py-2 border border-[var(--border-light)] rounded-lg focus:outline-none focus:ring-2 focus:ring-[var(--primary-500)] bg-[var(--bg-secondary)] text-[var(--text-primary)]"
                  placeholder="请输入邮箱"
                />
              </div>

              <div className="flex justify-end pt-4">
                <button
                  type="submit"
                  disabled={loading}
                  className="btn btn-primary flex items-center gap-2"
                >
                  <Save className="w-4 h-4" />
                  {loading ? '保存中...' : '保存修改'}
                </button>
              </div>
            </form>
          </div>
        )}

        {activeTab === 'password' && (
          <div className="bg-[var(--bg-primary)] rounded-lg p-6 border border-[var(--border-light)]">
            <h2 className="text-lg font-medium text-[var(--text-primary)] mb-4 flex items-center gap-2">
              <Key className="w-5 h-5" />
              修改密码
            </h2>

            {passwordError && (
              <div className="alert alert-error mb-4 rounded-lg">
                {passwordError}
              </div>
            )}

            <form onSubmit={handlePasswordSubmit} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                  原密码
                </label>
                <input
                  type="password"
                  value={passwordForm.old_password}
                  onChange={(e) => setPasswordForm({ ...passwordForm, old_password: e.target.value })}
                  className="w-full px-4 py-2 border border-[var(--border-light)] rounded-lg focus:outline-none focus:ring-2 focus:ring-[var(--primary-500)] bg-[var(--bg-secondary)] text-[var(--text-primary)]"
                  placeholder="请输入原密码"
                  required
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                  新密码
                </label>
                <input
                  type="password"
                  value={passwordForm.new_password}
                  onChange={(e) => setPasswordForm({ ...passwordForm, new_password: e.target.value })}
                  className="w-full px-4 py-2 border border-[var(--border-light)] rounded-lg focus:outline-none focus:ring-2 focus:ring-[var(--primary-500)] bg-[var(--bg-secondary)] text-[var(--text-primary)]"
                  placeholder="请输入新密码（至少6个字符）"
                  minLength={6}
                  required
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                  确认新密码
                </label>
                <input
                  type="password"
                  value={passwordForm.confirm_password}
                  onChange={(e) => setPasswordForm({ ...passwordForm, confirm_password: e.target.value })}
                  className="w-full px-4 py-2 border border-[var(--border-light)] rounded-lg focus:outline-none focus:ring-2 focus:ring-[var(--primary-500)] bg-[var(--bg-secondary)] text-[var(--text-primary)]"
                  placeholder="请再次输入新密码"
                  required
                />
              </div>

              <div className="flex justify-end pt-4">
                <button
                  type="submit"
                  disabled={loading}
                  className="btn btn-primary flex items-center gap-2"
                >
                  <Lock className="w-4 h-4" />
                  {loading ? '修改中...' : '修改密码'}
                </button>
              </div>
            </form>
          </div>
        )}

        {activeTab === 'telegram' && (
          <div className="bg-[var(--bg-primary)] rounded-lg p-6 border border-[var(--border-light)]">
            <h2 className="text-lg font-medium text-[var(--text-primary)] mb-4 flex items-center gap-2">
              <Bell className="w-5 h-5" />
              Telegram 通知
            </h2>

            <p className="text-sm text-[var(--text-tertiary)] mb-6">
              配置 Telegram Bot 后，新邮件将自动转发到指定的 Telegram 聊天。
            </p>

            {telegramError && (
              <div className="alert alert-error mb-4 rounded-lg">
                {telegramError}
              </div>
            )}

            <form onSubmit={handleTelegramSubmit} className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <label className="block text-sm font-medium text-[var(--text-secondary)]">
                    启用通知
                  </label>
                  <p className="text-xs text-[var(--text-tertiary)] mt-0.5">
                    开启后新邮件将自动转发到 Telegram
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() => setTelegramConfig({ ...telegramConfig, enabled: !telegramConfig.enabled })}
                  className={`relative w-11 h-6 rounded-full transition-colors ${
                    telegramConfig.enabled ? 'bg-[var(--primary-500)]' : 'bg-[var(--bg-tertiary)]'
                  }`}
                >
                  <span
                    className={`toggle-thumb absolute top-0.5 left-0.5 w-5 h-5 rounded-full transition-transform ${
                      telegramConfig.enabled ? 'translate-x-5' : 'translate-x-0'
                    }`}
                  />
                </button>
              </div>

              <div>
                <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                  Bot Token
                </label>
                <input
                  type="text"
                  value={telegramConfig.bot_token}
                  onChange={(e) => setTelegramConfig({ ...telegramConfig, bot_token: e.target.value })}
                  className="w-full px-4 py-2 border border-[var(--border-light)] rounded-lg focus:outline-none focus:ring-2 focus:ring-[var(--primary-500)] bg-[var(--bg-secondary)] text-[var(--text-primary)] font-mono text-sm"
                  placeholder="123456789:ABCdefGHIjklMNOpqrsTUVwxyz"
                />
                <p className="mt-1 text-xs text-[var(--text-tertiary)]">
                  从 @BotFather 获取
                </p>
              </div>

              <div>
                <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                  Chat ID
                </label>
                <input
                  type="text"
                  value={telegramConfig.chat_id}
                  onChange={(e) => setTelegramConfig({ ...telegramConfig, chat_id: e.target.value })}
                  className="w-full px-4 py-2 border border-[var(--border-light)] rounded-lg focus:outline-none focus:ring-2 focus:ring-[var(--primary-500)] bg-[var(--bg-secondary)] text-[var(--text-primary)]"
                  placeholder="-1001234567890"
                />
                <p className="mt-1 text-xs text-[var(--text-tertiary)]">
                  可以是用户 ID、群组 ID 或频道 ID
                </p>
              </div>

              <div className="flex justify-between pt-4">
                <button
                  type="button"
                  onClick={handleTelegramTest}
                  disabled={telegramTesting || !telegramConfig.bot_token || !telegramConfig.chat_id}
                  className="px-4 py-2 rounded-lg text-sm text-[var(--primary-700)] border border-[var(--primary-200)] hover:bg-[var(--bg-accent-soft)] transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {telegramTesting ? '测试中...' : '发送测试消息'}
                </button>
                <button
                  type="submit"
                  disabled={telegramLoading}
                  className="btn btn-primary flex items-center gap-2"
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
