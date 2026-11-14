import { AxiosError, AxiosHeaders, type AxiosResponse } from 'axios'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { emailApi, type EmailDetail, type EmailSummary, type EmailListResponse, type RefreshEmailsResponse } from '../src/api'
import { MailStore } from '../src/mail/MailStore'

const summary: EmailSummary = { id: 1, account_id: 1, created_at: '2026-01-01', date: '2026-01-01', from: 'sender@example.com', from_name: 'Sender', to: 'me@example.com', subject: 'First', has_attachment: false, is_read: true, folder: 'INBOX' }
const detail: EmailDetail = { ...summary, body_html: '<p>完整正文</p>', body_text: '完整正文' }
const response: AxiosResponse = { data: null, status: 200, statusText: 'OK', headers: {}, config: { headers: new AxiosHeaders() } }
const stores: MailStore[] = []

afterEach(() => { stores.forEach(store => store.dispose()); stores.length = 0 })

function deferred<T>() {
  return Promise.withResolvers<T>()
}
function setup() {
  const api = {
    ...emailApi,
    list: vi.fn<typeof emailApi.list>().mockResolvedValue({ data: [summary], total: 1 }),
    get: vi.fn<typeof emailApi.get>().mockResolvedValue({ data: detail }),
    delete: vi.fn<typeof emailApi.delete>().mockResolvedValue(response),
    markAsRead: vi.fn<typeof emailApi.markAsRead>().mockResolvedValue({ message: 'ok', sync_pending: false }),
    markAsUnread: vi.fn<typeof emailApi.markAsUnread>().mockResolvedValue({ message: 'ok', sync_pending: false }),
    refresh: vi.fn<typeof emailApi.refresh>().mockResolvedValue({ message: 'ok', new_count: 0 }),
  }
  const store = new MailStore(api)
  stores.push(store)
  return { store, api }
}

async function settle() {
  // Deterministically flush the finite promise chains without wall-clock waits.
  for (let index = 0; index < 12; index++) await Promise.resolve()
}

describe('mail query and detail lifecycle', () => {
  it('ignores an older query response even if the transport ignores abort', async () => {
    const { store, api } = setup()
    const old = deferred<EmailListResponse>()
    const recent = deferred<EmailListResponse>()
    api.list.mockReturnValueOnce(old.promise).mockReturnValueOnce(recent.promise)
    store.setQuery({ search: 'old' })
    store.setQuery({ search: 'new' })
    expect(api.list.mock.calls[0][1]?.aborted).toBe(true)
    recent.resolve({ data: [{ ...summary, id: 2, subject: 'new' }], total: 1 })
    await settle()
    old.resolve({ data: [summary], total: 1 })
    await settle()
    expect(store.getSnapshot().emails.map(email => email.subject)).toEqual(['new'])
  })

  it('finishes refresh against the current query and clears selection on scope changes', async () => {
    const { store, api } = setup()
    store.setQuery({ search: 'old' })
    await settle()
    store.setSelectedIds(new Set([1]))
    const refresh = deferred<RefreshEmailsResponse>()
    api.refresh.mockReturnValueOnce(refresh.promise)
    const running = store.refresh()
    store.setQuery({ search: 'new', account_id: 2, unread: true })
    expect(store.getSnapshot().selectedIds.size).toBe(0)
    refresh.resolve({ message: 'ok', new_count: 1 })
    await running
    expect(api.list.mock.calls.at(-1)?.[0]).toEqual({ search: 'new', account_id: 2, unread: true })
  })

  it('patches selected detail and its cache without replacing the body with a summary', async () => {
    const { store, api } = setup()
    await store.reload()
    await store.open(summary)
    await store.setRead(summary, false)
    expect(store.getSnapshot().selectedDetail?.body_html).toBe(detail.body_html)
    store.closeDetail()
    await store.open(summary, false)
    expect(store.getSnapshot().selectedDetail).toMatchObject({ body_html: detail.body_html, is_read: false })
    api.list.mockResolvedValue({ data: [{ ...summary, is_read: false, subject: 'Updated' }], total: 1 })
    await store.reload()
    expect(store.getSnapshot().selectedDetail).toMatchObject({ body_html: detail.body_html, is_read: false, subject: 'Updated' })
    expect(api.get).toHaveBeenCalledTimes(1)
  })

  it('deduplicates active detail requests, cancels unused ones and retries failures', async () => {
    const { store, api } = setup()
    const first = deferred<{ data: EmailDetail }>()
    api.get.mockReturnValueOnce(first.promise)
    const loading = store.open(summary, false)
    expect(store.open(summary, false)).toBe(loading)
    expect(api.get).toHaveBeenCalledTimes(1)
    store.closeDetail()
    expect(api.get.mock.calls[0][1]?.aborted).toBe(true)
    first.resolve({ data: detail })
    await loading
    expect(store.getSnapshot().selectedDetail).toBeNull()
    api.get.mockRejectedValueOnce(new Error('offline'))
    await store.open(summary, false)
    expect(store.getSnapshot().detailError).toContain('重试')
    expect(store.getSnapshot().selectedDetail).toBeNull()
    await store.retryDetail()
    expect(api.get).toHaveBeenCalledTimes(3)
    expect(store.getSnapshot().selectedDetail?.body_html).toBe(detail.body_html)
    expect(store.getSnapshot().detailError).toBeNull()
  })

  it('rolls back failed optimistic read and prevents a late body response from restoring stale state', async () => {
    const { store, api } = setup()
    const unread = { ...summary, is_read: false }
    const body = deferred<{ data: EmailDetail }>()
    api.list.mockResolvedValue({ data: [unread], total: 1 })
    api.get.mockReturnValueOnce(body.promise)
    api.markAsRead.mockRejectedValueOnce(new Error('offline'))
    await store.reload()
    const loading = store.open(unread)
    expect(store.getSnapshot().selectedEmail?.is_read).toBe(true)
    await settle()
    expect(store.getSnapshot().selectedEmail?.is_read).toBe(false)
    body.resolve({ data: { ...detail, is_read: true } })
    await loading
    expect(store.getSnapshot().selectedDetail?.is_read).toBe(false)
    expect(store.getSnapshot().notice?.type).toBe('error')
  })

  it('rolls back queued read failures to confirmed server state', async () => {
    const { store, api } = setup()
    const unread = { ...summary, is_read: false }
    api.list.mockResolvedValue({ data: [unread], total: 1 })
    api.markAsRead.mockRejectedValue(new Error('offline'))
    api.markAsUnread.mockRejectedValue(new Error('offline'))
    await store.reload()
    const first = store.setRead(unread, true)
    const second = store.setRead(unread, false)
    await Promise.all([first, second])
    expect(store.getSnapshot().emails[0].is_read).toBe(false)
  })

  it('reports saved pending synchronization and handles mailbox epoch conflicts', async () => {
    const { store, api } = setup()
    await store.reload()
    api.markAsUnread.mockResolvedValueOnce({ message: 'ok', sync_pending: true })
    await store.setRead(summary, false)
    expect(store.getSnapshot().notice?.message).toBe('已保存，等待同步')
    api.markAsRead.mockRejectedValueOnce(new AxiosError('conflict', '409', undefined, undefined, { ...response, status: 409 }))
    api.list.mockResolvedValue({ data: [{ ...summary, id: 2 }], total: 1 })
    await store.setRead(summary, true)
    await settle()
    expect(store.getSnapshot().notice?.message).toContain('邮箱已变化')
    expect(store.getSnapshot().emails.map(email => email.id)).toEqual([2])
    expect(store.getSnapshot().selectedEmail).toBeNull()
  })
})

describe('mail deletion lifecycle', () => {
  it('keeps pending deletion hidden during refresh and supports undo', async () => {
    vi.useFakeTimers()
    const { store, api } = setup()
    await store.reload()
    store.scheduleDelete(summary)
    await store.reload()
    expect(store.getSnapshot().emails).toEqual([])
    store.getSnapshot().notice?.onAction?.()
    await settle()
    await vi.advanceTimersByTimeAsync(2300)
    expect(api.delete).not.toHaveBeenCalled()
    expect(store.getSnapshot().emails.map(email => email.id)).toEqual([1])
  })

  it('restores a failed queued delete even if the recovery request is offline', async () => {
    vi.useFakeTimers()
    const { store, api } = setup()
    await store.reload()
    store.scheduleDelete(summary)
    api.delete.mockRejectedValueOnce(new Error('offline'))
    api.list.mockRejectedValue(new Error('offline'))
    await vi.advanceTimersByTimeAsync(2300)
    expect(store.getSnapshot().emails.map(email => email.id)).toEqual([1])
    expect(store.getSnapshot().notice?.type).toBe('error')
  })

  it('retains failed batch selection and accurately reports partial failure', async () => {
    const { store, api } = setup()
    const second = { ...summary, id: 2 }
    api.list.mockResolvedValue({ data: [summary, second], total: 2 })
    api.delete.mockImplementation(async id => { if (id === 2) throw new Error('offline'); return response })
    await store.reload()
    store.setSelectedIds(new Set([1, 2]))
    await store.batchDelete()
    expect([...store.getSnapshot().selectedIds]).toEqual([2])
    expect(store.getSnapshot().emails.map(email => email.id)).toEqual([2])
    expect(store.getSnapshot().notice).toMatchObject({ type: 'error', message: '已删除 1 封，1 封删除失败，请重试' })
  })

  it('restores failed deletion rows even when the recovery reload fails', async () => {
    const { store, api } = setup()
    await store.reload()
    store.setSelectedIds(new Set([1]))
    const deletion = deferred<AxiosResponse>()
    api.delete.mockReturnValueOnce(deletion.promise)
    const running = store.batchDelete()
    await store.reload()
    expect(store.getSnapshot().emails).toEqual([])
    api.list.mockRejectedValue(new Error('offline'))
    deletion.reject(new Error('offline'))
    await running
    expect(store.getSnapshot().emails.map(email => email.id)).toEqual([1])
    expect([...store.getSnapshot().selectedIds]).toEqual([1])
  })

  it('does not attach old batch failures to a new query selection', async () => {
    const { store, api } = setup()
    store.setQuery({ search: 'old' })
    await settle()
    store.setSelectedIds(new Set([1]))
    const deletion = deferred<AxiosResponse>()
    api.delete.mockReturnValueOnce(deletion.promise)
    const running = store.batchDelete()
    api.list.mockResolvedValue({ data: [{ ...summary, id: 2 }], total: 1 })
    store.setQuery({ search: 'new' })
    await settle()
    deletion.reject(new Error('offline'))
    await running
    expect([...store.getSnapshot().selectedIds]).toEqual([])
    expect(store.getSnapshot().emails.map(email => email.id)).toEqual([2])
  })
})
