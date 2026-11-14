import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Link, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { AxiosError, AxiosHeaders } from 'axios'
import api, { authApi, emailApi, type AuthStatus, type EmailDetail, type EmailSummary } from '../src/api'
import { AuthProvider } from '../src/context/AuthContext'
import useAuth from '../src/context/useAuth'
import MailProvider from '../src/mail/MailProvider'
import useMail from '../src/mail/useMail'
import ConfirmDialog from '../src/components/ConfirmDialog'

const summary: EmailSummary = { id: 1, account_id: 1, created_at: '2026-01-01', date: '2026-01-01', from: 'sender@example.com', from_name: 'Sender', to: 'me@example.com', subject: 'First', has_attachment: false, folder: 'INBOX' }
const detail: EmailDetail = { ...summary, body_text: 'Current session body', body_html: '' }

function Inbox() {
  const { emails, selectedDetail, loadingDetail, store } = useMail({})
  return <div><p>收件箱页面</p>{emails.map(email => <button key={email.id} type="button" onClick={() => void store.open(email)}>打开 {email.subject}</button>)}{loadingDetail && <p>正在加载正文</p>}{selectedDetail && <p>{selectedDetail.body_text}</p>}</div>
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
  vi.spyOn(emailApi, 'list').mockResolvedValue({ data: [summary], total: 1 })
  return vi.spyOn(emailApi, 'get').mockResolvedValue({ data: detail })
}

describe('mail session ownership', () => {
  it('retains a password-free session body cache across routes', async () => {
    const get = mockMail()
    render(<Workspace />)
    fireEvent.click(await screen.findByRole('button', { name: '打开 First' }))
    await screen.findByText('Current session body')
    expect(localStorage.getItem('token')).toBeNull()
    fireEvent.click(screen.getByRole('link', { name: '设置' }))
    expect(screen.getByText('设置页面')).toBeTruthy()
    fireEvent.click(screen.getByRole('link', { name: '返回收件箱' }))
    fireEvent.click(await screen.findByRole('button', { name: '打开 First' }))
    expect(screen.getByText('Current session body')).toBeTruthy()
    expect(get).toHaveBeenCalledTimes(1)
  })

  it('cancels unused detail on route changes and fetches it again when reopened', async () => {
    const get = mockMail()
    const pending = Promise.withResolvers<{ data: EmailDetail }>()
    get.mockReturnValueOnce(pending.promise)
    render(<Workspace />)
    fireEvent.click(await screen.findByRole('button', { name: '打开 First' }))
    expect(screen.getByText('正在加载正文')).toBeTruthy()
    fireEvent.click(screen.getByRole('link', { name: '设置' }))
    expect(get.mock.calls[0][1]?.aborted).toBe(true)
    await act(async () => pending.resolve({ data: { ...detail, body_text: 'Obsolete body' } }))
    fireEvent.click(screen.getByRole('link', { name: '返回收件箱' }))
    fireEvent.click(await screen.findByRole('button', { name: '打开 First' }))
    await screen.findByText('Current session body')
    expect(screen.queryByText('Obsolete body')).toBeNull()
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('isolates pending detail when logout and a new login share a render and token', async () => {
    const get = mockMail(true)
    const pending = Promise.withResolvers<{ data: EmailDetail }>()
    get.mockReturnValueOnce(pending.promise)
    // Session ownership must change even when the server returns the same token.
    vi.spyOn(authApi, 'login').mockResolvedValue({ token: 'session-a' })
    render(<Workspace />)
    fireEvent.click(await screen.findByRole('button', { name: '打开 First' }))
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: '退出并重新登录' })) })
    expect(localStorage.getItem('token')).toBe('session-a')
    fireEvent.click(await screen.findByRole('button', { name: '打开 First' }))
    await screen.findByText('Current session body')
    await act(async () => pending.resolve({ data: { ...detail, body_text: 'Previous session body' } }))
    expect(screen.queryByText('Previous session body')).toBeNull()
    expect(screen.getByText('Current session body')).toBeTruthy()
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('invalidates detail immediately on 401 while status is pending and starts a fresh cache after login', async () => {
    const get = mockMail(true)
    const pendingBody = Promise.withResolvers<{ data: EmailDetail }>()
    const pendingStatus = Promise.withResolvers<AuthStatus>()
    get.mockReturnValueOnce(pendingBody.promise)
    vi.mocked(authApi.status).mockResolvedValueOnce({ password_required: true, authenticated: true }).mockReturnValueOnce(pendingStatus.promise)
    vi.spyOn(authApi, 'login').mockResolvedValue({ token: 'replacement-session' })
    render(<Workspace />)
    fireEvent.click(await screen.findByRole('button', { name: '打开 First' }))
    await act(async () => {
      await api.get('/emails', { adapter: async config => {
        throw new AxiosError('expired', 'ERR_BAD_REQUEST', config, undefined, { data: {}, status: 401, statusText: 'Unauthorized', headers: new AxiosHeaders(), config })
      } }).catch(() => undefined)
    })
    expect(localStorage.getItem('token')).toBeNull()
    expect(screen.queryByText('收件箱页面')).toBeNull()
    await act(async () => pendingBody.resolve({ data: { ...detail, body_text: 'Expired session body' } }))
    await act(async () => pendingStatus.resolve({ password_required: true, authenticated: false }))
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: '重新登录' })) })
    expect(localStorage.getItem('token')).toBe('replacement-session')
    fireEvent.click(await screen.findByRole('button', { name: '打开 First' }))
    await screen.findByText('Current session body')
    expect(screen.queryByText('Expired session body')).toBeNull()
    expect(get).toHaveBeenCalledTimes(2)
  })
})

describe('confirmation dialog focus', () => {
  it('moves focus inside, traps both tab directions and restores the invoking control', async () => {
    const user = userEvent.setup()
    const cancel = vi.fn()
    const { rerender } = render(<><button type="button">删除账户</button><ConfirmDialog open={false} title="删除确认" message="确定删除账户？" onCancel={cancel} onConfirm={() => {}} /></>)
    const trigger = screen.getByRole('button', { name: '删除账户' })
    trigger.focus()
    rerender(<><button type="button">删除账户</button><ConfirmDialog open title="删除确认" message="确定删除账户？" onCancel={cancel} onConfirm={() => {}} /></>)
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
    rerender(<><button type="button">删除账户</button><ConfirmDialog open={false} title="删除确认" message="确定删除账户？" onCancel={cancel} onConfirm={() => {}} /></>)
    await waitFor(() => expect(document.activeElement).toBe(trigger))
  })
})
