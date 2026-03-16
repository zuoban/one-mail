import { useState, useEffect, useMemo, useCallback, type ReactNode } from 'react'
import { authApi, type User } from '../api'
import AuthContext from './auth-context'

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [token, setToken] = useState<string | null>(() => localStorage.getItem('token'))
  const [loading, setLoading] = useState(() => Boolean(localStorage.getItem('token')))

  useEffect(() => {
    if (!token || user) return
    authApi.me()
      .then(res => {
        setUser(res.data)
      })
      .catch(() => {
        localStorage.removeItem('token')
        setToken(null)
      })
      .finally(() => setLoading(false))
  }, [token, user])

  const login = useCallback((newToken: string, newUser: User) => {
    localStorage.setItem('token', newToken)
    setToken(newToken)
    setUser(newUser)
  }, [])

  const logout = useCallback(() => {
    localStorage.removeItem('token')
    setToken(null)
    setUser(null)
  }, [])

  const updateUser = useCallback((updatedUser: User) => {
    setUser(updatedUser)
  }, [])

  const value = useMemo(() => ({
    user,
    token,
    loading,
    login,
    logout,
    updateUser,
  }), [user, token, loading, login, logout, updateUser])

  return (
    <AuthContext.Provider value={value}>
      {children}
    </AuthContext.Provider>
  )
}