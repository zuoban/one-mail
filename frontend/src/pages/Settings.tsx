import { useState, useEffect } from 'react'
import { authApi } from '../api'
import useAuth from '../context/useAuth'
import { User, Lock, Save, Key } from 'lucide-react'
import axios from 'axios'

export default function Settings() {
  const { user, updateUser } = useAuth()
  const [loading, setLoading] = useState(false)
  const [activeTab, setActiveTab] = useState<'profile' | 'password'>('profile')
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

  useEffect(() => {
    if (user) {
      setProfileForm({
        username: user.username,
        email: user.email,
      })
    }
  }, [user])

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

  return (
    <div className="h-full overflow-auto">
      <div className="max-w-4xl mx-auto p-8">
        <h1 className="text-2xl font-semibold text-[var(--text-primary)] mb-8">设置</h1>

        {toast && (
          <div
            className={`fixed top-4 right-4 px-4 py-3 rounded-lg shadow-lg z-50 ${
              toast.type === 'success'
                ? 'bg-green-500 text-white'
                : 'bg-red-500 text-white'
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
        </div>

        {activeTab === 'profile' && (
          <div className="bg-[var(--bg-primary)] rounded-lg p-6 border border-[var(--border-light)]">
            <h2 className="text-lg font-medium text-[var(--text-primary)] mb-4 flex items-center gap-2">
              <User className="w-5 h-5" />
              个人信息
            </h2>

            {profileError && (
              <div className="mb-4 p-3 bg-red-50 border border-red-200 text-red-600 rounded-lg text-sm">
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
              <div className="mb-4 p-3 bg-red-50 border border-red-200 text-red-600 rounded-lg text-sm">
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
      </div>
    </div>
  )
}
