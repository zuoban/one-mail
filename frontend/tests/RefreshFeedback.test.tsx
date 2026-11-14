import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AxiosAdapter } from 'axios'
import apiClient, { accountApi, emailApi, type EmailAccount, type EmailSummary, type RefreshEmailsResponse } from '../src/api'
import ThemeContext from '../src/context/theme-context'
import { MailContext } from '../src/mail/mail-context'
import { MailStore } from '../src/mail/MailStore'
import Dashboard from '../src/pages/Dashboard'
import Accounts from '../src/pages/Accounts'

const account: EmailAccount = { id: 1, email: 'reader@example.com', provider: 'custom', username: 'reader@example.com', status: 'active', last_sync_time: '2026-11-14T08:00:00Z', color: '#2563eb' }
const email: EmailSummary = { id: 1, account_id: 1, created_at: '2026-11-14', date: '2026-11-14', from: 'sender@example.com', from_name: 'Sender', to: account.email, subject: '会议安排', has_attachment: false, folder: 'INBOX' }
const stores: MailStore[] = []

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class {
    observe() {}
    disconnect() {}
    unobserve() {}
  })
  vi.spyOn(accountApi, 'list').mockResolvedValue({ data: [account] })
})

afterEach(() => {
  stores.forEach(store => store.dispose())
  stores.length = 0
  vi.unstubAllGlobals()
})

function renderInbox(emails = [email]) {
  const pending = Promise.withResolvers<RefreshEmailsResponse>()
  const api = {
    ...emailApi,
    list: vi.fn<typeof emailApi.list>().mockResolvedValue({ data: emails, total: emails.length }),
    get: vi.fn<typeof emailApi.get>().mockResolvedValue({ data: { ...email, body_text: '已加载的邮件正文', body_html: '' } }),
    refresh: vi.fn<typeof emailApi.refresh>().mockReturnValue(pending.promise),
  }
  const store = new MailStore(api)
  stores.push(store)
  const view = render(<ThemeContext.Provider value={{ theme: 'light', resolvedTheme: 'light', setTheme: vi.fn() }}><MailContext.Provider value={store}><Dashboard /></MailContext.Provider></ThemeContext.Provider>)
  return { pending, api, view }
}

describe('refresh feedback', () => {
  it('keeps the selected message readable during refresh and restores the button on success', async () => {
    const { pending, api } = renderInbox()
    fireEvent.click(await screen.findByRole('group', { name: 'Sender，会议安排' }))
    await screen.findByText('已加载的邮件正文')
    const refresh = screen.getByRole('button', { name: '刷新' }) as HTMLButtonElement
    await waitFor(() => expect(refresh.disabled).toBe(false))
    fireEvent.click(refresh)
    expect(screen.getByRole('button', { name: '刷新中' }).getAttribute('aria-busy')).toBe('true')
    expect(refresh.disabled).toBe(true)
    expect(screen.getByText('正在获取最近 50 封邮件')).toBeTruthy()
    expect(screen.getByText('已加载的邮件正文')).toBeTruthy()
    expect(screen.getByRole('group', { name: 'Sender，会议安排' })).toBeTruthy()
    fireEvent.click(refresh)
    expect(api.refresh).toHaveBeenCalledTimes(1)
    await act(async () => pending.resolve({ message: 'ok', new_count: 0 }))
    await screen.findByText('已是最新邮件')
    expect(refresh.disabled).toBe(false)
    expect(refresh.getAttribute('aria-busy')).toBe('false')
    expect(screen.getByText('已加载的邮件正文')).toBeTruthy()
  })

  it('shows progress instead of an empty state and recovers when refresh fails', async () => {
    const { pending } = renderInbox([])
    await screen.findByText('暂无邮件')
    const refresh = screen.getByRole('button', { name: '刷新' }) as HTMLButtonElement
    await waitFor(() => expect(refresh.disabled).toBe(false))
    fireEvent.click(refresh)
    expect(screen.getByText('正在刷新邮件')).toBeTruthy()
    expect(screen.queryByText('暂无邮件')).toBeNull()
    await act(async () => pending.reject(new Error('offline')))
    expect((await screen.findByRole('alert')).textContent).toContain('刷新失败')
    expect(screen.getByText('暂无邮件')).toBeTruthy()
    expect(refresh.disabled).toBe(false)
    expect(screen.queryByText('正在获取最近 50 封邮件')).toBeNull()
  })

  it.each(['success', 'failure'] as const)('clears account progress after %s', async outcome => {
    const pending = Promise.withResolvers<RefreshEmailsResponse>()
    vi.spyOn(accountApi, 'refresh').mockReturnValue(pending.promise)
    render(<Accounts />)
    const refresh = await screen.findByRole('button', { name: '刷新' }) as HTMLButtonElement
    fireEvent.click(refresh)
    expect(refresh.disabled).toBe(true)
    expect(screen.getByRole('button', { name: '刷新中' }).getAttribute('aria-busy')).toBe('true')
    expect(screen.getByText('正在获取最近 50 封邮件')).toBeTruthy()
    await act(async () => {
      if (outcome === 'success') pending.resolve({ message: 'ok', new_count: 1 })
      else pending.reject(new Error('offline'))
    })
    await screen.findByText(outcome === 'success' ? '已刷新，新增 1 封邮件' : '刷新失败')
    expect(refresh.disabled).toBe(false)
    expect(refresh.getAttribute('aria-busy')).toBe('false')
    expect(screen.queryByText('正在获取最近 50 封邮件')).toBeNull()
    expect(screen.getByText(/上次刷新:/)).toBeTruthy()
  })

  it.each([false, true])('reports failed list GET after sync, including partial sync errors: %s', async partial => {
    const { pending, api } = renderInbox()
    await screen.findByRole('group', { name: 'Sender，会议安排' })
    api.list.mockRejectedValueOnce(new Error('read offline'))
    fireEvent.click(screen.getByRole('button', { name: '刷新' }))
    await act(async () => pending.resolve({ message: 'ok', new_count: 1, ...(partial ? { errors: { 'other@example.com': 'timeout' } } : {}) }))
    const notice = screen.getAllByRole('alert').find(element => element.textContent?.includes('邮箱同步请求已完成'))
    expect(notice?.textContent).toContain('邮件列表未能更新')
    if (partial) expect(notice?.textContent).toContain('other@example.com 刷新失败: timeout')
    expect(screen.queryByText('已刷新，新增 1 封邮件')).toBeNull()
    expect(screen.getByRole('group', { name: 'Sender，会议安排' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    await waitFor(() => expect(screen.queryByText('邮件列表加载失败，请重试')).toBeNull())
    expect(api.refresh).toHaveBeenCalledTimes(1)
    expect(api.list).toHaveBeenCalledTimes(3)
  })

  it('explains an account metadata GET failure after successful mail refresh and retries only accounts', async () => {
    const { pending, api } = renderInbox()
    await screen.findByRole('group', { name: 'Sender，会议安排' })
    vi.mocked(accountApi.list).mockRejectedValueOnce(new Error('account read offline'))
    fireEvent.click(screen.getByRole('button', { name: '刷新' }))
    await act(async () => pending.resolve({ message: 'ok', new_count: 1 }))
    await screen.findByText('账户列表未能更新，请重试读取账户')
    expect(screen.queryByText('已刷新，新增 1 封邮件')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '重试读取账户' }))
    await waitFor(() => expect(screen.queryByText('账户列表加载失败，请重试')).toBeNull())
    expect(api.refresh).toHaveBeenCalledTimes(1)
    expect(accountApi.list).toHaveBeenCalledTimes(3)
  })

  it('shows initial account load errors and recovers with an account GET', async () => {
    const initial = Promise.withResolvers<{ data: EmailAccount[] }>()
    vi.mocked(accountApi.list).mockReturnValueOnce(initial.promise)
    const { api } = renderInbox()
    expect(screen.getByText('正在加载账户列表…')).toBeTruthy()
    await act(async () => initial.reject(new Error('offline')))
    expect(screen.getByText('账户列表加载失败，请重试')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '重试读取账户' }))
    await waitFor(() => expect((screen.getByRole('button', { name: '刷新' }) as HTMLButtonElement).disabled).toBe(false))
    expect(api.refresh).not.toHaveBeenCalled()
    expect(accountApi.list).toHaveBeenCalledTimes(2)
  })

  it('aborts pending account loading when the dashboard unmounts', async () => {
    const initial = Promise.withResolvers<{ data: EmailAccount[] }>()
    vi.mocked(accountApi.list).mockReturnValueOnce(initial.promise)
    const { view } = renderInbox()
    view.unmount()
    expect(vi.mocked(accountApi.list).mock.calls[0][0]?.aborted).toBe(true)
    await act(async () => initial.resolve({ data: [account] }))
    expect(screen.queryByText('所有邮箱')).toBeNull()
  })
})

describe('read-only inbox', () => {
  it('opens and reopens a message using only GET requests', async () => {
    const previousAdapter = apiClient.defaults.adapter
    const adapter = vi.fn<AxiosAdapter>(async config => {
      const data = config.url === '/emails'
        ? { data: [email], total: 1 }
        : { data: { ...email, body_text: '正文来自只读请求', body_html: '' } }
      return { data, status: 200, statusText: 'OK', headers: {}, config }
    })
    apiClient.defaults.adapter = adapter
    const store = new MailStore()
    stores.push(store)
    try {
      render(<ThemeContext.Provider value={{ theme: 'light', resolvedTheme: 'light', setTheme: vi.fn() }}><MailContext.Provider value={store}><Dashboard /></MailContext.Provider></ThemeContext.Provider>)
      fireEvent.click(await screen.findByRole('group', { name: 'Sender，会议安排' }))
      await screen.findByText('正文来自只读请求')
      fireEvent.keyDown(window, { key: 'Escape' })
      fireEvent.click(screen.getByRole('group', { name: 'Sender，会议安排' }))
      expect(screen.getByText('正文来自只读请求')).toBeTruthy()
      expect(adapter.mock.calls.map(([config]) => ({ method: config.method, url: config.url }))).toEqual([
        { method: 'get', url: '/emails' },
        { method: 'get', url: '/emails/1' },
      ])
    } finally {
      apiClient.defaults.adapter = previousAdapter
    }
  })

  it('removes mutation controls and shortcuts while preserving navigation, search and refresh', async () => {
    const second = { ...email, id: 2, subject: '另一封邮件', date: '2026-11-13' }
    const { pending, api } = renderInbox([email, second])
    const firstRow = await screen.findByRole('group', { name: 'Sender，会议安排' })
    const secondRow = screen.getByRole('group', { name: 'Sender，另一封邮件' })
    firstRow.scrollIntoView = vi.fn()
    secondRow.scrollIntoView = vi.fn()
    fireEvent.click(firstRow)
    await screen.findByText('已加载的邮件正文')
    expect(screen.queryByRole('button', { name: /删除|已读|未读|批量|全选|反选|撤销/ })).toBeNull()
    expect(screen.queryByRole('checkbox')).toBeNull()
    expect(screen.queryByRole('group', { name: '邮件阅读状态' })).toBeNull()
    expect(screen.queryByText(/未读/)).toBeNull()
    fireEvent.contextMenu(firstRow)
    expect(screen.queryByRole('menu')).toBeNull()
    for (const key of ['u', 'U', 'Backspace', 'Delete']) fireEvent.keyDown(window, { key })
    expect(firstRow.getAttribute('aria-current')).toBe('true')
    expect(secondRow.getAttribute('aria-current')).toBeNull()
    expect(screen.getAllByRole('group')).toHaveLength(2)
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(api.get).toHaveBeenCalledTimes(1)
    fireEvent.keyDown(window, { key: 'j' })
    await waitFor(() => expect(secondRow.getAttribute('aria-current')).toBe('true'))
    fireEvent.keyDown(window, { key: 'k' })
    await waitFor(() => expect(firstRow.getAttribute('aria-current')).toBe('true'))
    fireEvent.keyDown(window, { key: '?' })
    const shortcuts = screen.getByRole('dialog')
    expect(shortcuts.textContent).not.toMatch(/删除|已读|未读/)
    expect(shortcuts.textContent).toContain('转发到 Telegram')
    fireEvent.keyDown(window, { key: 'Escape' })
    fireEvent.keyDown(window, { key: '/' })
    const search = screen.getByRole('textbox', { name: '搜索发件人或主题' })
    expect(document.activeElement).toBe(search)
    fireEvent.change(search, { target: { value: '会议' } })
    fireEvent.keyDown(search, { key: 'Enter' })
    await waitFor(() => expect(api.list.mock.calls.at(-1)?.[0]?.search).toBe('会议'))
    fireEvent.keyDown(search, { key: 'Escape' })
    fireEvent.keyDown(window, { key: 'r' })
    await waitFor(() => expect(api.refresh).toHaveBeenCalledTimes(1))
    await act(async () => pending.resolve({ message: 'ok', new_count: 0 }))
    await screen.findByText('已是最新邮件')
    expect(api.list.mock.calls.at(-1)?.[0]?.search).toBe('会议')
    expect(screen.getAllByRole('group')).toHaveLength(2)
  })
})
