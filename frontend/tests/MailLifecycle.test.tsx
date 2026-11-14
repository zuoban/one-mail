import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Link, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { AxiosError, AxiosHeaders } from 'axios'
import api, { authApi, emailApi, type AuthStatus } from '../src/api'
import { AuthProvider } from '../src/context/AuthContext'
import useAuth from '../src/context/useAuth'
import MailProvider from '../src/mail/MailProvider'
import useMail from '../src/mail/useMail'
import ConfirmDialog from '../src/components/ConfirmDialog'

function Inbox() {
  const { emails, store } = useMail({})
  return <div><p>收件箱页面</p>{emails.map(email => <button key={email.id} type="button" onClick={() => store.scheduleDelete(email)}>删除 {email.subject}</button>)}</div>
}
function SessionControls() {
  const { login, logout } = useAuth()
  return <>
    <button type="button" onClick={() => { logout(); void login('password') }}>退出并重新登录</button>
    <button type="button" onClick={() => void login('password')}>重新登录</button>
  </>
}
function Workspace() {
  return <AuthProvider><MemoryRouter><SessionControls /><MailProvider><Link to="/settings">设置</Link><Link to="/">返回收件箱</Link><Routes><Route path="/" element={<Inbox />} /><Route path="/settings" element={<p>设置页面</p>} /></Routes></MailProvider></MemoryRouter></AuthProvider>
}

function mockMail(passwordRequired = false) {
  if (passwordRequired) localStorage.setItem('token', 'session-a')
  vi.spyOn(authApi, 'status').mockResolvedValue({ password_required: passwordRequired, authenticated: true })
  vi.spyOn(emailApi, 'list').mockResolvedValue({ data: [{ id: 1, account_id: 1, created_at: '2026-01-01', date: '2026-01-01', from: 'sender@example.com', from_name: 'Sender', to: 'me@example.com', subject: 'First', has_attachment: false, is_read: true, folder: 'INBOX' }], total: 1 })
  return vi.spyOn(emailApi, 'delete').mockResolvedValue({ data: null, status: 200, statusText: 'OK', headers: {}, config: { headers: new AxiosHeaders() } })
}

describe('mail session ownership', () => {
  it('commits password-free delayed deletion across routes and filters a refresh before commit', async () => {
    const deletion = mockMail()
    render(<Workspace />)
    const remove = await screen.findByRole('button', { name: '删除 First' })
    expect(localStorage.getItem('token')).toBeNull()
    vi.useFakeTimers()
    fireEvent.click(remove)
    fireEvent.click(screen.getByRole('link', { name: '设置' }))
    expect(screen.getByText('设置页面')).toBeTruthy()
    fireEvent.click(screen.getByRole('link', { name: '返回收件箱' }))
    await act(async () => { await Promise.resolve(); await Promise.resolve() })
    expect(screen.queryByRole('button', { name: '删除 First' })).toBeNull()
    fireEvent.click(screen.getByRole('link', { name: '设置' }))
    await act(async () => { await vi.advanceTimersByTimeAsync(2300) })
    expect(deletion).toHaveBeenCalledTimes(1)
    expect(deletion.mock.calls[0][0]).toBe(1)
  })

  it('invalidates the old queue when logout and a new login share a render', async () => {
    const deletion = mockMail(true)
    // A new owner must be distinct even if a server returns the same token again.
    vi.spyOn(authApi, 'login').mockResolvedValue({ token: 'session-a' })
    render(<Workspace />)
    const remove = await screen.findByRole('button', { name: '删除 First' })
    vi.useFakeTimers()
    fireEvent.click(remove)
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: '退出并重新登录' })) })
    expect(localStorage.getItem('token')).toBe('session-a')
    await act(async () => { await vi.advanceTimersByTimeAsync(2300) })
    expect(deletion).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: '删除 First' })).toBeTruthy()
  })

  it('immediately invalidates a 401 session queue while status is pending and keeps it invalid after login', async () => {
    const deletion = mockMail(true)
    const pendingStatus = Promise.withResolvers<AuthStatus>()
    vi.mocked(authApi.status).mockResolvedValueOnce({ password_required: true, authenticated: true }).mockReturnValueOnce(pendingStatus.promise)
    vi.spyOn(authApi, 'login').mockResolvedValue({ token: 'replacement-session' })
    render(<Workspace />)
    const remove = await screen.findByRole('button', { name: '删除 First' })
    vi.useFakeTimers()
    fireEvent.click(remove)
    await act(async () => {
      await api.get('/emails', { adapter: async config => {
        throw new AxiosError('expired', 'ERR_BAD_REQUEST', config, undefined, { data: {}, status: 401, statusText: 'Unauthorized', headers: new AxiosHeaders(), config })
      } }).catch(() => undefined)
    })
    expect(localStorage.getItem('token')).toBeNull()
    await act(async () => { await vi.advanceTimersByTimeAsync(2300) })
    expect(deletion).not.toHaveBeenCalled()
    await act(async () => pendingStatus.resolve({ password_required: true, authenticated: false }))
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: '重新登录' })) })
    await act(async () => { await vi.advanceTimersByTimeAsync(2300) })
    expect(localStorage.getItem('token')).toBe('replacement-session')
    expect(deletion).not.toHaveBeenCalled()
  })
})

describe('confirmation dialog focus', () => {
  it('moves focus inside, traps both tab directions and restores the invoking control', async () => {
    const user = userEvent.setup()
    const cancel = vi.fn()
    const { rerender } = render(<><button type="button">删除邮件</button><ConfirmDialog open={false} title="删除确认" message="确定删除？" onCancel={cancel} onConfirm={() => {}} /></>)
    const trigger = screen.getByRole('button', { name: '删除邮件' })
    trigger.focus()
    rerender(<><button type="button">删除邮件</button><ConfirmDialog open title="删除确认" message="确定删除？" onCancel={cancel} onConfirm={() => {}} /></>)
    const cancelButton = screen.getByRole('button', { name: '取消' })
    const confirmButton = screen.getByRole('button', { name: '确认' })
    expect(document.activeElement).toBe(cancelButton)
    await user.tab({ shift: true })
    expect(document.activeElement).toBe(confirmButton)
    await user.tab()
    expect(document.activeElement).toBe(cancelButton)
    trigger.focus()
    expect(document.activeElement).toBe(cancelButton)
    await user.keyboard('{Escape}')
    expect(cancel).toHaveBeenCalledTimes(1)
    rerender(<><button type="button">删除邮件</button><ConfirmDialog open={false} title="删除确认" message="确定删除？" onCancel={cancel} onConfirm={() => {}} /></>)
    await waitFor(() => expect(document.activeElement).toBe(trigger))
  })
})
