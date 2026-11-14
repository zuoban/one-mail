import { StrictMode, useState, type ReactNode } from 'react'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AxiosResponse } from 'axios'
import { accountApi, providerApi, type EmailAccount, type RefreshEmailsResponse } from '../src/api'
import Accounts from '../src/pages/Accounts'
import { AccountStore } from '../src/accounts/AccountStore'
import { AccountContext } from '../src/accounts/account-context'

const account: EmailAccount = { id: 1, email: 'reader@example.com', provider: 'custom', username: 'reader@example.com', status: 'active', last_sync_time: '', color: '#2563eb', imap_host: 'imap.example.com', imap_port: 993 }
const second: EmailAccount = { ...account, id: 2, email: 'second@example.com' }

beforeEach(() => {
  vi.spyOn(accountApi, 'list').mockResolvedValue({ data: [account] })
  vi.spyOn(providerApi, 'list').mockResolvedValue({ data: [
    { id: 'gmail', label: 'Gmail', custom: false, imap_host: 'imap.gmail.com', imap_port: 993 },
    { id: 'custom', label: '自定义', custom: true, imap_host: '', imap_port: 993 },
  ] })
})

function AccountSession({ children }: { children: ReactNode }) {
  const [store] = useState(() => new AccountStore())
  return <AccountContext.Provider value={store}>{children}</AccountContext.Provider>
}

function renderAccounts(strict = false) {
  return render(strict ? <StrictMode><AccountSession><Accounts /></AccountSession></StrictMode> : <AccountSession><Accounts /></AccountSession>)
}

function openForm() {
  fireEvent.click(screen.getByRole('button', { name: '添加账户' }))
  const dialog = screen.getByRole('dialog', { name: '添加邮箱账户' })
  fireEvent.change(within(dialog).getByLabelText('邮箱地址'), { target: { value: 'new@example.com' } })
  fireEvent.change(within(dialog).getByLabelText('密码 / 授权码'), { target: { value: 'secret' } })
  return dialog
}

describe('account request lifecycle', () => {
  it('shows loading and an actionable error before showing a true empty state', async () => {
    const initial = Promise.withResolvers<{ data: EmailAccount[] }>()
    vi.mocked(accountApi.list).mockReturnValueOnce(initial.promise).mockResolvedValueOnce({ data: [] })
    renderAccounts()
    expect(screen.getByText('正在加载账户列表…')).toBeTruthy()
    expect(screen.queryByText('暂无邮箱账户')).toBeNull()
    await act(async () => initial.reject(new Error('offline')))
    expect(screen.getByRole('alert').textContent).toContain('账户列表加载失败')
    expect(screen.queryByText('暂无邮箱账户')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '重试读取账户' }))
    expect(await screen.findByText('暂无邮箱账户')).toBeTruthy()
  })

  it('aborts the first StrictMode load and ignores its late response', async () => {
    const stale = Promise.withResolvers<{ data: EmailAccount[] }>()
    const current = Promise.withResolvers<{ data: EmailAccount[] }>()
    vi.mocked(accountApi.list).mockReturnValueOnce(stale.promise).mockReturnValueOnce(current.promise)
    const view = renderAccounts(true)
    expect(vi.mocked(accountApi.list).mock.calls[0][0]?.aborted).toBe(true)
    await act(async () => current.resolve({ data: [second] }))
    await act(async () => stale.resolve({ data: [account] }))
    expect(screen.getByText(second.email)).toBeTruthy()
    expect(screen.queryByText(account.email)).toBeNull()
    view.unmount()
  })

  it('aborts an account read on unmount even if its transport later resolves', async () => {
    const pending = Promise.withResolvers<{ data: EmailAccount[] }>()
    vi.mocked(accountApi.list).mockReturnValueOnce(pending.promise)
    const view = renderAccounts()
    view.unmount()
    expect(vi.mocked(accountApi.list).mock.calls[0][0]?.aborted).toBe(true)
    await act(async () => pending.resolve({ data: [account] }))
    expect(screen.queryByText(account.email)).toBeNull()
  })

  it('keeps each account pending independently and prevents duplicate refreshes', async () => {
    vi.mocked(accountApi.list).mockResolvedValue({ data: [account, second] })
    const firstRefresh = Promise.withResolvers<RefreshEmailsResponse>()
    const secondRefresh = Promise.withResolvers<RefreshEmailsResponse>()
    const refresh = vi.spyOn(accountApi, 'refresh').mockImplementation(id => id === 1 ? firstRefresh.promise : secondRefresh.promise)
    renderAccounts()
    await screen.findByText(second.email)
    const buttons = screen.getAllByRole('button', { name: '刷新' }) as HTMLButtonElement[]
    fireEvent.click(buttons[0])
    fireEvent.click(buttons[1])
    fireEvent.click(buttons[0])
    expect(refresh).toHaveBeenCalledTimes(2)
    expect(screen.getAllByRole('button', { name: '刷新中' })).toHaveLength(2)
    await act(async () => firstRefresh.resolve({ message: 'ok', new_count: 0 }))
    expect(buttons[0].disabled).toBe(false)
    expect(buttons[1].disabled).toBe(true)
    fireEvent.click(buttons[1])
    expect(refresh).toHaveBeenCalledTimes(2)
    await act(async () => secondRefresh.reject(new Error('offline')))
    expect(buttons[1].disabled).toBe(false)
  })

  it('orders overlapping account reloads and keeps only the latest response', async () => {
    vi.mocked(accountApi.list).mockResolvedValueOnce({ data: [account, second] })
    const older = Promise.withResolvers<{ data: EmailAccount[] }>()
    const newer = Promise.withResolvers<{ data: EmailAccount[] }>()
    vi.mocked(accountApi.list).mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)
    vi.spyOn(accountApi, 'refresh').mockResolvedValue({ message: 'ok', new_count: 0 })
    renderAccounts()
    await screen.findByText(second.email)
    const buttons = screen.getAllByRole('button', { name: '刷新' })
    fireEvent.click(buttons[0])
    await waitFor(() => expect(accountApi.list).toHaveBeenCalledTimes(2))
    fireEvent.click(buttons[1])
    await waitFor(() => expect(accountApi.list).toHaveBeenCalledTimes(3))
    expect(vi.mocked(accountApi.list).mock.calls[1][0]?.aborted).toBe(true)
    await act(async () => newer.resolve({ data: [second] }))
    await act(async () => older.resolve({ data: [account] }))
    expect(screen.getByText(second.email)).toBeTruthy()
    expect(screen.queryByText(account.email)).toBeNull()
  })

  it('explains a successful sync followed by a failed GET and retries only the GET', async () => {
    const refresh = vi.spyOn(accountApi, 'refresh').mockResolvedValue({ message: 'ok', new_count: 1 })
    vi.mocked(accountApi.list).mockResolvedValueOnce({ data: [account] }).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ data: [account] })
    renderAccounts()
    fireEvent.click(await screen.findByRole('button', { name: '刷新' }))
    await screen.findByText('邮箱同步已完成，但账户列表读取失败，请重试读取列表')
    expect(screen.queryByText('已刷新，新增 1 封邮件')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '重试读取账户' }))
    await waitFor(() => expect(screen.queryByText('账户列表加载失败，请重试')).toBeNull())
    expect(refresh).toHaveBeenCalledTimes(1)
    expect(accountApi.list).toHaveBeenCalledTimes(3)
  })

  it('keeps a saved account distinct from a failed list GET and retries without another submit', async () => {
    const add = vi.spyOn(accountApi, 'add').mockResolvedValue({ data: account })
    vi.mocked(accountApi.list).mockResolvedValueOnce({ data: [account] }).mockRejectedValueOnce(new Error('read offline'))
    renderAccounts()
    await screen.findByText(account.email)
    const dialog = openForm()
    fireEvent.click(within(dialog).getByRole('button', { name: '添加账户' }))
    await screen.findByText('账户已保存，但账户列表读取失败，请重试读取列表')
    expect(screen.queryByRole('dialog')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '重试读取账户' }))
    await waitFor(() => expect(screen.queryByText('账户列表加载失败，请重试')).toBeNull())
    expect(add).toHaveBeenCalledTimes(1)
    expect(accountApi.list).toHaveBeenCalledTimes(3)
  })

  it('does not let a saved form list callback change a newly opened form', async () => {
    vi.spyOn(accountApi, 'add').mockResolvedValue({ data: account })
    const read = Promise.withResolvers<{ data: EmailAccount[] }>()
    vi.mocked(accountApi.list).mockResolvedValueOnce({ data: [account] }).mockReturnValueOnce(read.promise)
    renderAccounts()
    await screen.findByText(account.email)
    const old = openForm()
    fireEvent.click(within(old).getByRole('button', { name: '添加账户' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    const current = openForm()
    fireEvent.change(within(current).getByLabelText('邮箱地址'), { target: { value: 'current@example.com' } })
    await act(async () => read.reject(new Error('read offline')))
    expect(screen.getByRole('dialog')).toBe(current)
    expect((within(current).getByLabelText('邮箱地址') as HTMLInputElement).value).toBe('current@example.com')
    expect(within(current).queryByRole('alert')).toBeNull()
    expect(screen.queryByText('账户已保存，但账户列表读取失败，请重试读取列表')).toBeNull()
    expect((within(current).getByRole('button', { name: '添加账户' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('locks editing and every close route during submit, then preserves input on failure', async () => {
    const pending = Promise.withResolvers<{ data: EmailAccount }>()
    const add = vi.spyOn(accountApi, 'add').mockReturnValue(pending.promise)
    renderAccounts()
    await screen.findByText(account.email)
    const dialog = openForm()
    const submit = within(dialog).getByRole('button', { name: '添加账户' })
    fireEvent.click(submit)
    fireEvent.submit(submit.closest('form')!)
    expect(add).toHaveBeenCalledTimes(1)
    expect(within(dialog).getByLabelText('邮箱地址').matches(':disabled')).toBe(true)
    expect((within(dialog).getByRole('button', { name: '关闭' }) as HTMLButtonElement).disabled).toBe(true)
    expect((within(dialog).getByRole('button', { name: '取消' }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.keyDown(window, { key: 'Escape' })
    fireEvent.click(document.querySelector('.overlay-backdrop')!)
    expect(screen.getByRole('dialog')).toBe(dialog)
    await act(async () => pending.reject(new Error('offline')))
    expect(within(dialog).getByRole('alert').textContent).toContain('添加失败')
    expect((within(dialog).getByLabelText('邮箱地址') as HTMLInputElement).value).toBe('new@example.com')
    expect(within(dialog).getByLabelText('邮箱地址').matches(':disabled')).toBe(false)
  })

  it.each(['success', 'failure'] as const)('ignores an old submission after external unmount: %s', async outcome => {
    const pending = Promise.withResolvers<{ data: EmailAccount }>()
    vi.spyOn(accountApi, 'add').mockReturnValue(pending.promise)
    const first = renderAccounts()
    await screen.findByText(account.email)
    const old = openForm()
    fireEvent.click(within(old).getByRole('button', { name: '添加账户' }))
    first.unmount()
    renderAccounts()
    await screen.findByText(account.email)
    const current = openForm()
    fireEvent.change(within(current).getByLabelText('邮箱地址'), { target: { value: 'current@example.com' } })
    await act(async () => {
      if (outcome === 'success') pending.resolve({ data: account })
      else pending.reject(new Error('offline'))
    })
    expect(screen.getByRole('dialog')).toBe(current)
    expect((within(current).getByLabelText('邮箱地址') as HTMLInputElement).value).toBe('current@example.com')
    expect(within(current).queryByRole('alert')).toBeNull()
    expect(accountApi.list).toHaveBeenCalledTimes(2)
  })

  it('catches deletion failure, blocks duplicate and cancel actions, and allows retry', async () => {
    const pending = Promise.withResolvers<AxiosResponse>()
    const remove = vi.spyOn(accountApi, 'delete').mockReturnValueOnce(pending.promise).mockRejectedValueOnce(new Error('still offline'))
    renderAccounts()
    fireEvent.click(await screen.findByRole('button', { name: '删除账户' }))
    const dialog = screen.getByRole('dialog', { name: '删除账户' })
    const confirm = within(dialog).getByRole('button', { name: '删除' }) as HTMLButtonElement
    fireEvent.click(confirm)
    fireEvent.click(confirm)
    fireEvent.keyDown(window, { key: 'Escape' })
    fireEvent.click(document.querySelector('.overlay-backdrop')!)
    expect(confirm.disabled).toBe(true)
    expect(remove).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('dialog')).toBe(dialog)
    await act(async () => pending.reject(new Error('offline')))
    expect(within(dialog).getByRole('alert').textContent).toContain('删除失败，请重试')
    expect(confirm.disabled).toBe(false)
    fireEvent.click(confirm)
    await waitFor(() => expect(remove).toHaveBeenCalledTimes(2))
    expect(accountApi.list).toHaveBeenCalledTimes(1)
  })

  it('does not start a list reload when a pending refresh resolves after unmount', async () => {
    const pending = Promise.withResolvers<RefreshEmailsResponse>()
    vi.spyOn(accountApi, 'refresh').mockReturnValue(pending.promise)
    const view = renderAccounts()
    fireEvent.click(await screen.findByRole('button', { name: '刷新' }))
    view.unmount()
    await act(async () => pending.resolve({ message: 'ok', new_count: 1 }))
    expect(accountApi.list).toHaveBeenCalledTimes(1)
  })

  it('saves custom IMAP host and numeric port for new and edited accounts', async () => {
    const add = vi.spyOn(accountApi, 'add').mockResolvedValue({ data: account })
    const update = vi.spyOn(accountApi, 'update').mockResolvedValue({ data: account })
    renderAccounts()
    await screen.findByText(account.email)
    const dialog = openForm()
    fireEvent.change(within(dialog).getByLabelText('邮箱服务商'), { target: { value: 'custom' } })
    fireEvent.change(within(dialog).getByLabelText('IMAP 主机'), { target: { value: 'mail.private.example' } })
    fireEvent.change(within(dialog).getByLabelText('IMAP 端口'), { target: { value: '1993' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '添加账户' }))
    await waitFor(() => expect(add).toHaveBeenCalledWith(expect.objectContaining({ provider: 'custom', imap_host: 'mail.private.example', imap_port: 1993 })))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    fireEvent.click(screen.getByRole('button', { name: '编辑账户' }))
    const edit = screen.getByRole('dialog', { name: '编辑邮箱账户' })
    expect((within(edit).getByLabelText('IMAP 主机') as HTMLInputElement).value).toBe('imap.example.com')
    expect((within(edit).getByLabelText('IMAP 端口') as HTMLInputElement).value).toBe('993')
    fireEvent.change(within(edit).getByLabelText('IMAP 主机'), { target: { value: 'moved.example.com' } })
    fireEvent.change(within(edit).getByLabelText('IMAP 端口'), { target: { value: '2993' } })
    fireEvent.click(within(edit).getByRole('button', { name: '保存修改' }))
    await waitFor(() => expect(update).toHaveBeenCalledWith(account.id, expect.objectContaining({ imap_host: 'moved.example.com', imap_port: 2993 })))
  })

  it('offers a real provider retry and prevents saving without authoritative options', async () => {
    vi.mocked(providerApi.list).mockRejectedValueOnce(new Error('offline'))
    renderAccounts()
    await screen.findByText(account.email)
    fireEvent.click(screen.getByRole('button', { name: '添加账户' }))
    const dialog = screen.getByRole('dialog', { name: '添加邮箱账户' })
    expect(await within(dialog).findByText('邮箱服务商加载失败，请重试')).toBeTruthy()
    expect(within(dialog).queryByRole('option', { name: 'Gmail' })).toBeNull()
    expect((within(dialog).getByRole('button', { name: '添加账户' }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(within(dialog).getByRole('button', { name: '重试读取服务商' }))
    expect(await within(dialog).findByRole('option', { name: '自定义' })).toBeTruthy()
    expect(providerApi.list).toHaveBeenCalledTimes(2)
    expect(within(dialog).queryByText('邮箱服务商加载失败，请重试')).toBeNull()
  })

  it('treats a saved account with failed initial sync as a completed creation', async () => {
    const add = vi.spyOn(accountApi, 'add').mockResolvedValue({ data: account, refresh_error: '首次同步失败' })
    renderAccounts()
    await screen.findByText(account.email)
    const dialog = openForm()
    fireEvent.click(within(dialog).getByRole('button', { name: '添加账户' }))
    await screen.findByText('账户已添加，但首次同步失败，可手动刷新')
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(add).toHaveBeenCalledTimes(1)
    expect(add.mock.calls[0][0]).not.toHaveProperty('imap_host')
    expect(add.mock.calls[0][0]).not.toHaveProperty('imap_port')
  })

})
