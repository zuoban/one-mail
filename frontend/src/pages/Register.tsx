import { useState } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import axios from 'axios'
import { authApi } from '../api'
import useAuth from '../context/useAuth'
import { useTheme } from '../context/ThemeContext'
import { Mail, Lock, User, UserPlus, AlertCircle, Sun, Moon } from 'lucide-react'

export default function Register() {
  const [username, setUsername] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const navigate = useNavigate()
  const { login } = useAuth()
  const { resolvedTheme, setTheme } = useTheme()

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')

    if (password !== confirmPassword) {
      setError('两次输入的密码不一致')
      return
    }

    if (password.length < 6) {
      setError('密码长度至少为 6 位')
      return
    }

    setLoading(true)

    try {
      const res = await authApi.register({ username, email, password })
      login(res.token, res.user)
      navigate('/inbox')
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setError(message || '注册失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="auth-shell bg-[var(--bg-secondary)] relative">
      <button
        type="button"
        onClick={() => setTheme(resolvedTheme === 'dark' ? 'light' : 'dark')}
        className="btn btn-secondary w-9 h-9 p-0 fixed top-4 right-4"
        aria-label={resolvedTheme === 'dark' ? '切换到浅色模式' : '切换到深色模式'}
        title={resolvedTheme === 'dark' ? '切换到浅色模式' : '切换到深色模式'}
      >
        {resolvedTheme === 'dark' ? (
          <Sun className="w-4 h-4 text-[var(--text-secondary)]" strokeWidth={1.5} />
        ) : (
          <Moon className="w-4 h-4 text-[var(--text-secondary)]" strokeWidth={1.5} />
        )}
      </button>
      <div className="auth-card rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] p-6 sm:p-8 shadow-[var(--shadow-sm)]">
        <div className="mb-6">
          <img
            src={resolvedTheme === 'dark' ? '/logo-dark.svg' : '/logo.svg'}
            alt="One-Mail"
            className="w-9 h-9 rounded-[8px] mb-4"
          />
          <h1 className="text-xl font-medium tracking-[-0.015em] text-[var(--text-primary)]">
            注册 One-Mail
          </h1>
          <p className="mt-1 text-sm text-[var(--text-secondary)]">
            创建用于管理个人邮箱的账号
          </p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          {error && (
            <div className="alert alert-error flex items-center gap-2">
              <AlertCircle className="w-4 h-4 flex-shrink-0" strokeWidth={1.5} />
              <span>{error}</span>
            </div>
          )}

          <div>
            <label className="block text-sm font-medium text-[var(--text-secondary)] mb-1.5">
              用户名
            </label>
            <div className="relative flex items-center">
              <User className="absolute left-3 w-4 h-4 text-[var(--text-tertiary)] pointer-events-none" strokeWidth={1.5} />
              <input
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                style={{ paddingLeft: '36px' }}
                placeholder="输入用户名"
                required
              />
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium text-[var(--text-secondary)] mb-1.5">
              邮箱
            </label>
            <div className="relative flex items-center">
              <Mail className="absolute left-3 w-4 h-4 text-[var(--text-tertiary)] pointer-events-none" strokeWidth={1.5} />
              <input
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                style={{ paddingLeft: '36px' }}
                placeholder="name@example.com"
                required
              />
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium text-[var(--text-secondary)] mb-1.5">
              密码
            </label>
            <div className="relative flex items-center">
              <Lock className="absolute left-3 w-4 h-4 text-[var(--text-tertiary)] pointer-events-none" strokeWidth={1.5} />
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                style={{ paddingLeft: '36px' }}
                placeholder="至少 6 位字符"
                required
              />
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium text-[var(--text-secondary)] mb-1.5">
              确认密码
            </label>
            <div className="relative flex items-center">
              <Lock className="absolute left-3 w-4 h-4 text-[var(--text-tertiary)] pointer-events-none" strokeWidth={1.5} />
              <input
                type="password"
                value={confirmPassword}
                onChange={(e) => setConfirmPassword(e.target.value)}
                style={{ paddingLeft: '36px' }}
                placeholder="再次输入密码"
                required
              />
            </div>
          </div>

          <button
            type="submit"
            disabled={loading}
            className="btn btn-primary w-full h-10 mt-2"
          >
            {loading ? (
              <span>注册中</span>
            ) : (
              <>
                <UserPlus className="w-4 h-4" strokeWidth={1.5} />
                <span>注册</span>
              </>
            )}
          </button>
        </form>

        <p className="mt-6 text-center text-sm text-[var(--text-secondary)]">
          已有账号？{' '}
          <Link to="/login" className="text-[var(--primary-600)] hover:underline font-medium">
            直接登录
          </Link>
        </p>
      </div>
    </div>
  )
}
