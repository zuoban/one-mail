import { useState } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import axios from 'axios'
import { authApi } from '../api'
import useAuth from '../context/useAuth'
import { useTheme } from '../context/ThemeContext'
import { Mail, Lock, LogIn, AlertCircle } from 'lucide-react'

export default function Login() {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const navigate = useNavigate()
  const { login } = useAuth()
  const { resolvedTheme } = useTheme()

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)

    try {
      const res = await authApi.login({ username, password })
      login(res.token, res.user)
      navigate('/inbox')
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setError(message || '用户名或密码错误，请重新输入')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="auth-shell bg-[var(--bg-secondary)]">
      <div className="auth-card card p-6 sm:p-8">
        <div className="text-center mb-6 sm:mb-8">
          <div className="inline-flex items-center justify-center w-14 h-14 sm:w-16 sm:h-16 rounded-full bg-[var(--primary-100)] mb-4">
            <img src={resolvedTheme === 'dark' ? '/logo-dark.svg' : '/logo.svg'} alt="One-Mail" className="w-10 h-10" />
          </div>
          <h1 className="text-xl sm:text-2xl font-semibold text-[var(--text-primary)] mb-2">登录 One-Mail</h1>
          <p className="text-[var(--text-secondary)]">欢迎回来</p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-5 sm:space-y-6">
          {error && (
            <div className="alert alert-error flex items-center gap-2 animate-pulse">
              <AlertCircle className="w-5 h-5 flex-shrink-0" />
              <span className="font-medium">{error}</span>
            </div>
          )}

          <div>
            <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
              用户名
            </label>
            <div className="relative">
              <Mail className="absolute left-3 top-1/2 -translate-y-1/2 w-5 h-5 text-[var(--text-tertiary)] pointer-events-none" />
              <input
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                style={{ paddingLeft: '44px' }}
                placeholder="请输入用户名"
                required
              />
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
              密码
            </label>
            <div className="relative">
              <Lock className="absolute left-3 top-1/2 -translate-y-1/2 w-5 h-5 text-[var(--text-tertiary)] pointer-events-none" />
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                style={{ paddingLeft: '44px' }}
                placeholder="请输入密码"
                required
              />
            </div>
          </div>

          <button
            type="submit"
            disabled={loading}
            className="w-full btn btn-primary py-3"
          >
            {loading ? (
              <span>登录中...</span>
            ) : (
              <>
                <LogIn className="w-5 h-5" />
                <span>登录</span>
              </>
            )}
          </button>
        </form>

        <p className="mt-6 text-center text-sm sm:text-base text-[var(--text-secondary)]">
          还没有账号？{' '}
          <Link to="/register" className="text-[var(--primary-600)] hover:text-[var(--primary-700)] font-medium">
            立即注册
          </Link>
        </p>
      </div>
    </div>
  )
}
