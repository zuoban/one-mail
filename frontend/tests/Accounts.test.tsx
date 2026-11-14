import { StrictMode, useState, type ReactNode } from 'react'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AxiosResponse } from 'axios'
import { accountApi, providerApi, type EmailAccount } from '../src/api'
import Accounts from '../src/pages/Accounts'
import { AccountStore } from '../src/accounts/AccountStore'
import { AccountContext } from '../src/accounts/account-context'

const account: EmailAccount = { id: 1, email: 'reader@example.com', provider: 'custom', username: 'reader@example.com', status: 'active', last_sync_time: '', imap_host: 'imap.example.com', imap_port: 993 }
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
  fireEvent.change(within(dialog).getByLabelText('邮箱地址'), { target: { value: 'new@gmail.com' } })
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

  it('shows account management actions without a refresh button', async () => {
    vi.mocked(accountApi.list).mockResolvedValue({ data: [account, second] })
    renderAccounts()
    await screen.findByText(second.email)
    expect(screen.queryByRole('button', { name: /刷新/ })).toBeNull()
    expect(screen.getAllByRole('button', { name: '测试连接' })).toHaveLength(2)
    expect(screen.getAllByRole('button', { name: '编辑账户' })).toHaveLength(2)
    expect(screen.getAllByRole('button', { name: '删除账户' })).toHaveLength(2)
  })

  it.each(['success', 'failure'] as const)('shows connection test progress and restores the button after %s', async outcome => {
    const pending = Promise.withResolvers<AxiosResponse>()
    const test = vi.spyOn(accountApi, 'test').mockReturnValueOnce(pending.promise).mockResolvedValue({ data: {} } as AxiosResponse)
    renderAccounts()
    const button = await screen.findByRole('button', { name: '测试连接' }) as HTMLButtonElement
    fireEvent.click(button)
    fireEvent.click(button)
    expect(button.disabled).toBe(true)
    expect(button.getAttribute('aria-busy')).toBe('true')
    expect(screen.getByText('正在测试连接…').closest('[role="status"]')).toBeTruthy()
    expect(test).toHaveBeenCalledExactlyOnceWith(account.id)
    await act(async () => {
      if (outcome === 'success') pending.resolve({ data: {} } as AxiosResponse)
      else pending.reject(new Error('offline'))
    })
    expect(button.disabled).toBe(false)
    expect(button.getAttribute('aria-busy')).toBe('false')
    expect(screen.queryByText('正在测试连接…')).toBeNull()
    expect(screen.getByText(outcome === 'success' ? '连接测试通过' : '连接失败')).toBeTruthy()
    await act(async () => { fireEvent.click(button) })
    expect(test).toHaveBeenCalledTimes(2)
    expect(button.disabled).toBe(false)
  })

  it('tracks simultaneous connection tests independently for each account', async () => {
    const firstTest = Promise.withResolvers<AxiosResponse>()
    const secondTest = Promise.withResolvers<AxiosResponse>()
    vi.mocked(accountApi.list).mockResolvedValue({ data: [account, second] })
    vi.spyOn(accountApi, 'test').mockImplementation(id => id === account.id ? firstTest.promise : secondTest.promise)
    renderAccounts()
    await screen.findByText(second.email)
    const [firstButton, secondButton] = screen.getAllByRole('button', { name: '测试连接' }) as HTMLButtonElement[]
    fireEvent.click(firstButton)
    expect(firstButton.disabled).toBe(true)
    expect(secondButton.disabled).toBe(false)
    fireEvent.click(secondButton)
    expect(screen.getAllByText('正在测试连接…')).toHaveLength(2)
    await act(async () => firstTest.resolve({ data: {} } as AxiosResponse))
    expect(firstButton.disabled).toBe(false)
    expect(secondButton.disabled).toBe(true)
    expect(screen.getAllByText('正在测试连接…')).toHaveLength(1)
    await act(async () => secondTest.reject(new Error('offline')))
    expect(secondButton.disabled).toBe(false)
    expect(screen.queryByText('正在测试连接…')).toBeNull()
    expect(screen.getByRole('alert').textContent).toContain('连接失败')
  })

  it('ignores a connection test result after leaving the account page', async () => {
    const pending = Promise.withResolvers<AxiosResponse>()
    vi.spyOn(accountApi, 'test').mockReturnValue(pending.promise)
    const first = renderAccounts()
    fireEvent.click(await screen.findByRole('button', { name: '测试连接' }))
    first.unmount()
    renderAccounts()
    const button = await screen.findByRole('button', { name: '测试连接' }) as HTMLButtonElement
    await act(async () => pending.resolve({ data: {} } as AxiosResponse))
    expect(button.disabled).toBe(false)
    expect(screen.queryByText('连接测试通过')).toBeNull()
    expect(screen.queryByText('正在测试连接…')).toBeNull()
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
    expect((within(dialog).getByLabelText('邮箱地址') as HTMLInputElement).value).toBe('new@gmail.com')
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

  it('saves custom IMAP host and numeric port for new and edited accounts', async () => {
    const add = vi.spyOn(accountApi, 'add').mockResolvedValue({ data: account })
    const update = vi.spyOn(accountApi, 'update').mockResolvedValue({ data: account })
    renderAccounts()
    await screen.findByText(account.email)
    const dialog = openForm()
    fireEvent.change(within(dialog).getByLabelText('邮箱地址'), { target: { value: 'new@company.example' } })
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
    fireEvent.change(within(dialog).getByLabelText('邮箱地址'), { target: { value: 'new@gmail.com' } })
    expect((within(dialog).getByRole('button', { name: '添加账户' }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(within(dialog).getByRole('button', { name: '重试读取服务商' }))
    await waitFor(() => expect((within(dialog).getByRole('button', { name: '添加账户' }) as HTMLButtonElement).disabled).toBe(false))
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

  it.each([
    ['reader@gmail.com', 'gmail'],
    ['reader@googlemail.com', 'gmail'],
    ['reader@ICLOUD.COM', 'icloud'],
    ['reader@me.com', 'icloud'],
    ['reader@mac.com', 'icloud'],
    ['reader@qq.com', 'qq'],
    ['reader@foxmail.com', 'qq'],
    ['reader@outlook.com', 'outlook'],
    ['reader@hotmail.com', 'outlook'],
    ['reader@live.com', 'outlook'],
    ['reader@msn.com', 'outlook'],
    ['reader@163.com', '163'],
  ])('automatically configures %s as %s', async (email, provider) => {
    const add = vi.spyOn(accountApi, 'add').mockResolvedValue({ data: account })
    vi.mocked(providerApi.list).mockResolvedValue({ data: [
      ...['gmail', 'icloud', 'qq', 'outlook', '163'].map(id => ({ id, label: id, custom: false, imap_host: `imap.${id}.example`, imap_port: 993 })),
      { id: 'custom', label: '自定义', custom: true, imap_host: '', imap_port: 993 },
    ] })
    renderAccounts()
    await screen.findByText(account.email)
    const dialog = openForm()
    fireEvent.change(within(dialog).getByLabelText('邮箱地址'), { target: { value: email } })
    expect(within(dialog).queryByRole('combobox')).toBeNull()
    expect(within(dialog).queryByLabelText('IMAP 主机')).toBeNull()
    fireEvent.click(within(dialog).getByRole('button', { name: '添加账户' }))
    await waitFor(() => expect(add).toHaveBeenCalledExactlyOnceWith({ email, provider, username: email, password: 'secret' }))
  })

  it('updates matching and custom fields without overwriting a manually entered username', async () => {
    const add = vi.spyOn(accountApi, 'add').mockResolvedValue({ data: account })
    renderAccounts()
    await screen.findByText(account.email)
    const dialog = openForm()
    fireEvent.change(within(dialog).getByLabelText('登录用户名'), { target: { value: 'login-name' } })
    fireEvent.change(within(dialog).getByLabelText('邮箱地址'), { target: { value: 'reader@gmail.com.example' } })
    const host = within(dialog).getByLabelText('IMAP 主机') as HTMLInputElement
    expect(host.value).toBe('')
    fireEvent.change(host, { target: { value: 'imap.private.example' } })
    fireEvent.change(within(dialog).getByLabelText('邮箱地址'), { target: { value: ' reader@GOOGLEMAIL.COM ' } })
    expect(within(dialog).queryByLabelText('IMAP 主机')).toBeNull()
    expect((within(dialog).getByLabelText('登录用户名') as HTMLInputElement).value).toBe('login-name')
    fireEvent.click(within(dialog).getByRole('button', { name: '添加账户' }))
    await waitFor(() => expect(add).toHaveBeenCalledExactlyOnceWith({ email: 'reader@GOOGLEMAIL.COM', provider: 'gmail', username: 'login-name', password: 'secret' }))
  })

  it('automatically shows iCloud password guidance and removes it after changing the email domain', async () => {
    vi.mocked(providerApi.list).mockResolvedValue({ data: [
      { id: 'icloud', label: 'iCloud', custom: false, imap_host: 'imap.mail.me.com', imap_port: 993 },
      { id: 'custom', label: '自定义', custom: true, imap_host: '', imap_port: 993 },
    ] })
    renderAccounts()
    await screen.findByText(account.email)
    fireEvent.click(screen.getByRole('button', { name: '添加账户' }))
    const dialog = screen.getByRole('dialog', { name: '添加邮箱账户' })
    expect(within(dialog).queryByLabelText('IMAP 主机')).toBeNull()
    fireEvent.change(within(dialog).getByLabelText('邮箱地址'), { target: { value: 'reader@me.com' } })
    expect(within(dialog).getByLabelText('App 专用密码')).toBeTruthy()
    expect(within(dialog).getByRole('link', { name: 'Apple 账户（新窗口）' }).getAttribute('href')).toBe('https://account.apple.com/')
    fireEvent.change(within(dialog).getByLabelText('邮箱地址'), { target: { value: 'reader@company.example' } })
    expect(within(dialog).getByLabelText('密码 / 授权码')).toBeTruthy()
    expect(within(dialog).queryByRole('link', { name: 'Apple 账户（新窗口）' })).toBeNull()
    expect(within(dialog).getByLabelText('IMAP 主机')).toBeTruthy()
  })

  it('preserves a saved custom server for a recognized email domain when editing', async () => {
    const customAccount = { ...account, email: 'reader@gmail.com' }
    vi.mocked(accountApi.list).mockResolvedValue({ data: [customAccount] })
    const update = vi.spyOn(accountApi, 'update').mockResolvedValue({ data: customAccount })
    renderAccounts()
    fireEvent.click(await screen.findByRole('button', { name: '编辑账户' }))
    const dialog = screen.getByRole('dialog', { name: '编辑邮箱账户' })
    expect((within(dialog).getByLabelText('IMAP 主机') as HTMLInputElement).value).toBe(account.imap_host)
    fireEvent.change(within(dialog).getByLabelText('登录用户名'), { target: { value: 'custom-login' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '保存修改' }))
    await waitFor(() => expect(update).toHaveBeenCalledExactlyOnceWith(account.id, { username: 'custom-login', imap_host: account.imap_host, imap_port: account.imap_port }))
  })

})
