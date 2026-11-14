import { useState, useEffect, useMemo, useCallback, useRef, type ReactNode } from 'react'
import { authApi, SESSION_EXPIRED_EVENT } from '../api'
import AuthContext from './auth-context'

export function AuthProvider({ children }: { children: ReactNode }) {
  const [passwordRequired, setPasswordRequired] = useState<boolean | null>(null)
  const [authenticated, setAuthenticated] = useState(false)
  const [token, setToken] = useState<string | null>(() => localStorage.getItem('token'))
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [sessionGeneration, setSessionGeneration] = useState(0)
  const session = useRef({ generation: 0, authenticated: false, passwordRequired: false, token: null as string | null })
  const request = useRef(0)

  const invalidate = useCallback(() => {
    const current = session.current
    current.generation += 1
    current.authenticated = false
    current.token = null
    setSessionGeneration(current.generation)
    setAuthenticated(false)
  }, [])

  const checkStatus = useCallback(async (currentRequest: number) => {
    try {
      const status = await authApi.status()
      if (currentRequest !== request.current) return
      const restoredToken = status.password_required && status.authenticated ? localStorage.getItem('token') : null
      if (!restoredToken) localStorage.removeItem('token')
      const current = session.current
      current.authenticated = status.authenticated
      current.passwordRequired = status.password_required
      current.token = restoredToken
      setPasswordRequired(status.password_required)
      setAuthenticated(status.authenticated)
      setToken(restoredToken)
    } catch {
      if (currentRequest !== request.current) return
      setError('无法确认访问状态，请检查网络后重试')
    } finally {
      if (currentRequest === request.current) setLoading(false)
    }
  }, [])

  const retry = useCallback(() => {
    const currentRequest = ++request.current
    invalidate()
    setLoading(true)
    setError(null)
    return checkStatus(currentRequest)
  }, [invalidate, checkStatus])

  const cancelPendingRequests = useCallback(() => {
    ++request.current
  }, [])

  useEffect(() => {
    // Keep one mutable owner for this provider lifetime, including effect cleanup.
    const currentSession = session.current
    const expire = () => {
      localStorage.removeItem('token')
      setToken(null)
      void retry()
    }
    window.addEventListener(SESSION_EXPIRED_EVENT, expire)
    void checkStatus(++request.current)
    return () => {
      window.removeEventListener(SESSION_EXPIRED_EVENT, expire)
      cancelPendingRequests()
      currentSession.authenticated = false
    }
  }, [retry, checkStatus, cancelPendingRequests])

  const login = useCallback(async (password: string) => {
    const currentRequest = ++request.current
    const result = await authApi.login({ password })
    if (currentRequest !== request.current) throw new Error('访问状态已变化，请重试')
    if (!result.token) {
      localStorage.removeItem('token')
      setToken(null)
      await retry()
      return
    }
    localStorage.setItem('token', result.token)
    const current = session.current
    current.generation += 1
    current.authenticated = true
    current.passwordRequired = true
    current.token = result.token
    setSessionGeneration(current.generation)
    setToken(result.token)
    setPasswordRequired(true)
    setAuthenticated(true)
    setError(null)
  }, [retry])

  const logout = useCallback(() => {
    ++request.current
    invalidate()
    localStorage.removeItem('token')
    setToken(null)
    setError(null)
    setLoading(false)
  }, [invalidate])

  const isSessionCurrent = useCallback((generation: number) => {
    const current = session.current
    return current.authenticated && current.generation === generation &&
      (!current.passwordRequired || localStorage.getItem('token') === current.token)
  }, [])

  const value = useMemo(() => ({
    passwordRequired, authenticated, token, loading, error, sessionGeneration,
    isSessionCurrent, retry, login, logout,
  }), [passwordRequired, authenticated, token, loading, error, sessionGeneration, isSessionCurrent, retry, login, logout])

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
