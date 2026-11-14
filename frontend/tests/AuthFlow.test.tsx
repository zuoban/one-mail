import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { AxiosError, AxiosHeaders } from 'axios'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import api, { authApi, type AuthStatus } from '../src/api'
import { AuthProvider } from '../src/context/AuthContext'
import useAuth from '../src/context/useAuth'
import ProtectedRoute from '../src/components/ProtectedRoute'
import Layout from '../src/components/Layout'
import Login from '../src/pages/Login'

vi.mock('../src/context/useTheme', () => ({ default: () => ({ resolvedTheme: 'light', setTheme: vi.fn() }) }))

function Workspace() {
  const location = useLocation()
  const { token } = useAuth()
  return <><h1>邮件工作区</h1><p data-testid="destination">{location.pathname}{location.search}{location.hash}</p><p data-testid="session-token">{token ?? 'none'}</p></>
}

function AccessApp({ path = '/inbox' }: { path?: string }) {
  return <AuthProvider><MemoryRouter initialEntries={[path]}><Routes>
    <Route path="/login" element={<Login />} />
    <Route element={<ProtectedRoute><Layout /></ProtectedRoute>}>
      <Route path="/inbox" element={<Workspace />} />
      <Route path="/accounts" element={<Workspace />} />
      <Route path="/settings" element={<Workspace />} />
    </Route>
  </Routes></MemoryRouter></AuthProvider>
}

function expiredRequest() {
  return api.get('/emails', { adapter: async config => {
    throw new AxiosError('expired', 'ERR_BAD_REQUEST', config, undefined, { data: { error: 'expired' }, status: 401, statusText: 'Unauthorized', headers: new AxiosHeaders(), config })
  } }).catch(() => undefined)
}

describe('personal workspace access', () => {
  it('checks status without a token and directly enters when no password is configured', async () => {
    const status = vi.spyOn(authApi, 'status').mockResolvedValue({ password_required: false, authenticated: true })
    render(<AccessApp />)
    await screen.findByRole('heading', { name: '邮件工作区' })
    expect(status).toHaveBeenCalledTimes(1)
    expect(screen.getByTestId('session-token').textContent).toBe('none')
    expect(localStorage.getItem('token')).toBeNull()
    expect(screen.queryByRole('button', { name: '退出登录' })).toBeNull()
  })

  it('redirects the login page into the workspace in password-free mode and discards an old token', async () => {
    localStorage.setItem('token', 'old-session')
    vi.spyOn(authApi, 'status').mockResolvedValue({ password_required: false, authenticated: true })
    render(<AccessApp path="/login" />)
    await screen.findByRole('heading', { name: '邮件工作区' })
    expect(screen.getByTestId('destination').textContent).toBe('/inbox')
    expect(localStorage.getItem('token')).toBeNull()
  })

  it('requires only an administrator password, reports rejection, and restores the original destination', async () => {
    vi.spyOn(authApi, 'status').mockResolvedValue({ password_required: true, authenticated: false })
    const login = vi.spyOn(authApi, 'login')
      .mockRejectedValueOnce(new AxiosError('invalid password', 'ERR_BAD_REQUEST', undefined, undefined, { data: {}, status: 401, statusText: 'Unauthorized', headers: new AxiosHeaders(), config: { headers: new AxiosHeaders() } }))
      .mockResolvedValueOnce({ token: 'new-session' })
    const { container } = render(<AccessApp path="/accounts?view=all#mailboxes" />)
    const password = await screen.findByLabelText('管理员密码')
    expect(container.querySelectorAll('input')).toHaveLength(1)
    fireEvent.change(password, { target: { value: 'wrong-password' } })
    fireEvent.click(screen.getByRole('button', { name: '登录' }))
    expect((await screen.findByRole('alert')).textContent).toContain('管理员密码错误')
    expect(screen.queryByRole('heading', { name: '邮件工作区' })).toBeNull()
    expect(localStorage.getItem('token')).toBeNull()
    fireEvent.change(password, { target: { value: 'correct-password' } })
    fireEvent.click(screen.getByRole('button', { name: '登录' }))
    await screen.findByRole('heading', { name: '邮件工作区' })
    expect(login).toHaveBeenNthCalledWith(1, { password: 'wrong-password' })
    expect(login).toHaveBeenNthCalledWith(2, { password: 'correct-password' })
    expect(screen.getByTestId('destination').textContent).toBe('/accounts?view=all#mailboxes')
    expect(localStorage.getItem('token')).toBe('new-session')
    expect(Object.values(localStorage)).not.toContain('correct-password')
    expect(screen.getAllByRole('button', { name: '退出登录' })).toHaveLength(2)
  })

  it('fails closed while status is pending or unavailable and supports retry', async () => {
    const pendingStatus = Promise.withResolvers<AuthStatus>()
    const status = vi.spyOn(authApi, 'status')
      .mockReturnValueOnce(pendingStatus.promise)
      .mockResolvedValueOnce({ password_required: false, authenticated: true })
    render(<AccessApp path="/settings" />)
    expect(screen.getByRole('status').textContent).toContain('正在确认访问状态')
    expect(screen.queryByRole('heading', { name: '邮件工作区' })).toBeNull()
    await act(async () => pendingStatus.reject(new Error('offline')))
    expect((await screen.findByRole('alert')).textContent).toContain('无法确认访问状态')
    expect(screen.queryByRole('heading', { name: '邮件工作区' })).toBeNull()
    expect(screen.queryByLabelText('管理员密码')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    await screen.findByRole('heading', { name: '邮件工作区' })
    expect(status).toHaveBeenCalledTimes(2)
    expect(screen.getByTestId('destination').textContent).toBe('/settings')
  })

  it('rechecks status when a deployment stops requiring a password during login', async () => {
    vi.spyOn(authApi, 'status')
      .mockResolvedValueOnce({ password_required: true, authenticated: false })
      .mockResolvedValueOnce({ password_required: false, authenticated: true })
    vi.spyOn(authApi, 'login').mockResolvedValue({ token: '' })
    render(<AccessApp />)
    fireEvent.change(await screen.findByLabelText('管理员密码'), { target: { value: 'previous-password' } })
    fireEvent.click(screen.getByRole('button', { name: '登录' }))
    await screen.findByRole('heading', { name: '邮件工作区' })
    expect(localStorage.getItem('token')).toBeNull()
    expect(screen.getByTestId('session-token').textContent).toBe('none')
    expect(screen.queryByRole('button', { name: '退出登录' })).toBeNull()
  })

  it('discards an invalid old token without treating the deployment as password-free', async () => {
    localStorage.setItem('token', 'invalid-old-token')
    vi.spyOn(authApi, 'status').mockResolvedValue({ password_required: true, authenticated: false })
    render(<AccessApp />)
    await screen.findByLabelText('管理员密码')
    expect(localStorage.getItem('token')).toBeNull()
    expect(screen.queryByRole('heading', { name: '邮件工作区' })).toBeNull()
  })

  it('restores a valid password session and redirects an already authenticated login page', async () => {
    localStorage.setItem('token', 'valid-session')
    vi.spyOn(authApi, 'status').mockResolvedValue({ password_required: true, authenticated: true })
    render(<AccessApp path="/login" />)
    await screen.findByRole('heading', { name: '邮件工作区' })
    expect(screen.getByTestId('session-token').textContent).toBe('valid-session')
    expect(screen.queryByLabelText('管理员密码')).toBeNull()
  })

  it('revokes access on a business API 401 and checks status before admitting another session', async () => {
    localStorage.setItem('token', 'expired-session')
    vi.spyOn(authApi, 'status')
      .mockResolvedValueOnce({ password_required: true, authenticated: true })
      .mockResolvedValueOnce({ password_required: true, authenticated: false })
    render(<AccessApp path="/settings" />)
    await screen.findByRole('heading', { name: '邮件工作区' })
    await act(async () => { await expiredRequest() })
    await screen.findByLabelText('管理员密码')
    expect(screen.queryByRole('heading', { name: '邮件工作区' })).toBeNull()
    expect(localStorage.getItem('token')).toBeNull()
    vi.spyOn(authApi, 'login').mockResolvedValue({ token: 'replacement-session' })
    fireEvent.change(screen.getByLabelText('管理员密码'), { target: { value: 'password' } })
    fireEvent.click(screen.getByRole('button', { name: '登录' }))
    await waitFor(() => expect(screen.getByTestId('destination').textContent).toBe('/settings'))
  })
})
