import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import type { AxiosResponse } from 'axios'
import { accountApi, authApi, providerApi, type EmailAccount } from '../src/api'
import { AuthProvider } from '../src/context/AuthContext'
import useAuth from '../src/context/useAuth'
import MailProvider from '../src/mail/MailProvider'
import { useAccounts } from '../src/accounts/account-context'
import Accounts from '../src/pages/Accounts'

const account: EmailAccount = { id: 1, email: 'reader@example.com', provider: 'custom', username: 'reader@example.com', status: 'active', last_sync_time: '', color: '#2563eb', imap_host: 'imap.example.com', imap_port: 993 }

function AccountInbox() {
  const { accounts, loadingAccounts, accountsError, loadAccounts } = useAccounts()
  return <section aria-label="共享账户收件箱">{loadingAccounts && <p>正在读取共享账户</p>}{accounts.map(account => <p key={account.id}>{account.email}</p>)}{accountsError && <button type="button" onClick={() => void loadAccounts()}>重试共享账户</button>}</section>
}

function SessionControls() {
  const { login, logout } = useAuth()
  return <button type="button" onClick={() => { logout(); void login('password') }}>切换会话</button>
}

function Workspace() {
  return <AuthProvider><MemoryRouter><SessionControls /><MailProvider><Link to="/">收件箱</Link><Link to="/accounts">账户管理</Link><Routes><Route path="/" element={<AccountInbox />} /><Route path="/accounts" element={<Accounts />} /></Routes></MailProvider></MemoryRouter></AuthProvider>
}

beforeEach(() => {
  vi.spyOn(authApi, 'status').mockResolvedValue({ password_required: false, authenticated: true })
  vi.spyOn(authApi, 'login').mockResolvedValue({ token: 'same-session-token' })
  vi.spyOn(accountApi, 'list').mockResolvedValue({ data: [account] })
  vi.spyOn(providerApi, 'list').mockResolvedValue({ data: [{ id: 'custom', label: '自定义', custom: true, imap_host: '', imap_port: 993 }] })
})

describe('shared account session', () => {
  it('reuses reads across routes and refreshes a mutation that finishes after leaving its form', async () => {
    const pending = Promise.withResolvers<{ data: EmailAccount }>()
    const changed = { ...account, email: 'changed@example.com' }
    vi.spyOn(accountApi, 'update').mockReturnValue(pending.promise)
    vi.mocked(accountApi.list).mockResolvedValueOnce({ data: [account] }).mockResolvedValue({ data: [changed] })
    render(<Workspace />)
    await screen.findByText(account.email)
    fireEvent.click(screen.getByRole('link', { name: '账户管理' }))
    expect(await screen.findByText(account.email)).toBeTruthy()
    expect(accountApi.list).toHaveBeenCalledTimes(1)
    expect(providerApi.list).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole('button', { name: '编辑账户' }))
    const form = screen.getByRole('dialog', { name: '编辑邮箱账户' })
    fireEvent.change(within(form).getByLabelText('邮箱地址'), { target: { value: changed.email } })
    fireEvent.click(within(form).getByRole('button', { name: '保存修改' }))
    fireEvent.click(screen.getByRole('link', { name: '收件箱' }))
    expect(screen.getByRole('region', { name: '共享账户收件箱' })).toBeTruthy()
    await act(async () => pending.resolve({ data: changed }))
    await screen.findByText(changed.email)
    expect(screen.queryByText(account.email)).toBeNull()
    expect(accountApi.list).toHaveBeenCalledTimes(2)
    fireEvent.click(screen.getByRole('link', { name: '账户管理' }))
    await screen.findByText(changed.email)
    expect(accountApi.list).toHaveBeenCalledTimes(2)
  })

  it('keeps deletion and a failed list reload distinct across route changes', async () => {
    vi.spyOn(accountApi, 'delete').mockResolvedValue({} as AxiosResponse)
    vi.mocked(accountApi.list).mockResolvedValueOnce({ data: [account] }).mockRejectedValueOnce(new Error('offline')).mockResolvedValue({ data: [] })
    render(<Workspace />)
    await screen.findByText(account.email)
    fireEvent.click(screen.getByRole('link', { name: '账户管理' }))
    fireEvent.click(await screen.findByRole('button', { name: '删除账户' }))
    fireEvent.click(within(screen.getByRole('dialog', { name: '删除账户' })).getByRole('button', { name: '删除' }))
    await screen.findByText('账户已删除，但账户列表读取失败，请重试读取列表')
    fireEvent.click(screen.getByRole('link', { name: '收件箱' }))
    fireEvent.click(screen.getByRole('button', { name: '重试共享账户' }))
    await waitFor(() => expect(screen.queryByText(account.email)).toBeNull())
    expect(accountApi.delete).toHaveBeenCalledTimes(1)
    expect(accountApi.list).toHaveBeenCalledTimes(3)
  })

  it('clears account state and ignores old pending reads when the session changes', async () => {
    const oldRead = Promise.withResolvers<{ data: EmailAccount[] }>()
    const replacement = { ...account, id: 2, email: 'next-session@example.com' }
    vi.mocked(accountApi.list).mockReturnValueOnce(oldRead.promise).mockResolvedValue({ data: [replacement] })
    render(<Workspace />)
    await screen.findByText('正在读取共享账户')
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: '切换会话' })) })
    await screen.findByText(replacement.email)
    expect(vi.mocked(accountApi.list).mock.calls[0][0]?.aborted).toBe(true)
    await act(async () => oldRead.resolve({ data: [account] }))
    expect(screen.queryByText(account.email)).toBeNull()
    expect(screen.getByText(replacement.email)).toBeTruthy()
    expect(accountApi.list).toHaveBeenCalledTimes(2)
    expect(providerApi.list).toHaveBeenCalledTimes(2)
  })

  it('prevents a previous session write from invalidating the replacement session', async () => {
    const oldWrite = Promise.withResolvers<{ data: EmailAccount }>()
    vi.spyOn(accountApi, 'update').mockReturnValueOnce(oldWrite.promise)
    render(<Workspace />)
    await screen.findByText(account.email)
    fireEvent.click(screen.getByRole('link', { name: '账户管理' }))
    fireEvent.click(await screen.findByRole('button', { name: '编辑账户' }))
    const form = screen.getByRole('dialog', { name: '编辑邮箱账户' })
    fireEvent.change(within(form).getByLabelText('IMAP 主机'), { target: { value: 'old-change.example.com' } })
    fireEvent.click(within(form).getByRole('button', { name: '保存修改' }))
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: '切换会话' })) })
    await screen.findByText(account.email)
    await act(async () => oldWrite.resolve({ data: { ...account, imap_host: 'old-change.example.com' } }))
    expect(accountApi.list).toHaveBeenCalledTimes(2)
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})
