import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Link, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { AxiosHeaders } from 'axios'
import { emailApi } from '../src/api'
import MailProvider from '../src/mail/MailProvider'
import useMail from '../src/mail/useMail'
import ConfirmDialog from '../src/components/ConfirmDialog'

const auth = vi.hoisted(() => ({ token: 'session-a' as string | null, user: { id: 1, username: 'reader', email: 'me@example.com' } }))
vi.mock('../src/context/useAuth', () => ({ default: () => auth }))

function Inbox() {
  const { emails, store } = useMail({})
  return <div><p>收件箱页面</p>{emails.map(email => <button key={email.id} type="button" onClick={() => store.scheduleDelete(email)}>删除 {email.subject}</button>)}</div>
}
function Workspace() {
  return <MemoryRouter><MailProvider><Link to="/settings">设置</Link><Link to="/">返回收件箱</Link><Routes><Route path="/" element={<Inbox />} /><Route path="/settings" element={<p>设置页面</p>} /></Routes></MailProvider></MemoryRouter>
}

function mockMail() {
  auth.token = 'session-a'
  localStorage.setItem('token', auth.token)
  vi.spyOn(emailApi, 'list').mockResolvedValue({ data: [{ id: 1, account_id: 1, created_at: '2026-01-01', date: '2026-01-01', from: 'sender@example.com', from_name: 'Sender', to: 'me@example.com', subject: 'First', has_attachment: false, is_read: true, folder: 'INBOX' }], total: 1 })
  return vi.spyOn(emailApi, 'delete').mockResolvedValue({ data: null, status: 200, statusText: 'OK', headers: {}, config: { headers: new AxiosHeaders() } })
}

describe('authenticated mail ownership', () => {
  it('commits delayed deletion across routes and filters a refresh before commit', async () => {
    const deletion = mockMail()
    render(<Workspace />)
    const remove = await screen.findByRole('button', { name: '删除 First' })
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

  it('cancels the old session queue immediately on logout or user switch', async () => {
    const deletion = mockMail()
    const { rerender } = render(<Workspace />)
    const remove = await screen.findByRole('button', { name: '删除 First' })
    vi.useFakeTimers()
    fireEvent.click(remove)
    auth.token = null
    localStorage.removeItem('token')
    rerender(<Workspace />)
    await act(async () => { await vi.advanceTimersByTimeAsync(2300) })
    expect(deletion).not.toHaveBeenCalled()
    auth.token = 'session-b'
    auth.user = { ...auth.user, id: 2 }
    localStorage.setItem('token', auth.token)
    rerender(<Workspace />)
    await act(async () => { await vi.advanceTimersByTimeAsync(2300) })
    expect(deletion).not.toHaveBeenCalled()
    auth.user = { ...auth.user, id: 1 }
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
