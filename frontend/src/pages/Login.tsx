import { useState } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import axios from 'axios'
import AuthStatusFeedback from '../components/AuthStatusFeedback'
import useAuth from '../context/useAuth'
import useTheme from '../context/useTheme'
import { Lock, LogIn, AlertCircle, Sun, Moon } from 'lucide-react'

export default function Login() {
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const location = useLocation()
  const { login, authenticated, passwordRequired, loading, error: statusError } = useAuth()
  const { resolvedTheme, setTheme } = useTheme()
  const from = (location.state as { from?: { pathname: string; search?: string; hash?: string } } | null)?.from
  const destination = from && from.pathname.startsWith('/') && !from.pathname.startsWith('//') && from.pathname !== '/login'
    ? `${from.pathname}${from.search || ''}${from.hash || ''}`
    : '/inbox'

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setSubmitting(true)

    try {
      await login(password)
      setPassword('')
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setError(message || (axios.isAxiosError(err) && err.response?.status === 401 ? '管理员密码错误，请重试' : '登录失败，请稍后重试'))
    } finally {
      setSubmitting(false)
    }
  }

  if (loading || statusError || passwordRequired === null) return <AuthStatusFeedback />
  if (authenticated) return <Navigate to={destination} replace />

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
            登录 One-Mail
          </h1>
          <p className="mt-1 text-sm text-[var(--text-secondary)]">
            输入管理员密码以进入邮件工作台
          </p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          {error && (
            <div role="alert" id="login-error" className="alert alert-error flex items-center gap-2">
              <AlertCircle className="w-4 h-4 flex-shrink-0" strokeWidth={1.5} />
              <span>{error}</span>
            </div>
          )}

          <div>
            <label htmlFor="admin-password" className="block text-sm font-medium text-[var(--text-secondary)] mb-1">
              管理员密码
            </label>
            <div className="relative flex items-center">
              <Lock className="absolute left-3 w-4 h-4 text-[var(--text-tertiary)] pointer-events-none" strokeWidth={1.5} />
              <input
                id="admin-password"
                autoComplete="current-password"
                autoFocus
                aria-invalid={Boolean(error)}
                aria-describedby={error ? 'login-error' : undefined}
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                style={{ paddingLeft: '36px' }}
                placeholder="输入管理员密码"
                required
              />
            </div>
          </div>

          <button
            type="submit"
            disabled={submitting}
            className="btn btn-primary w-full h-9"
          >
            {submitting ? (
              <span>登录中</span>
            ) : (
              <>
                <LogIn className="w-4 h-4" strokeWidth={1.5} />
                <span>登录</span>
              </>
            )}
          </button>
        </form>
      </div>
    </div>
  )
}
