import { afterEach, describe, expect, it, vi } from 'vitest'
import { emailApi, type EmailDetail, type EmailSummary, type EmailListResponse, type RefreshEmailsResponse } from '../src/api'
import { MailStore } from '../src/mail/MailStore'

const summary: EmailSummary = { id: 1, account_id: 1, created_at: '2026-01-01', date: '2026-01-01', from: 'sender@example.com', from_name: 'Sender', to: 'me@example.com', subject: 'First', has_attachment: false, folder: 'INBOX' }
const detail: EmailDetail = { ...summary, body_html: '<p>完整正文</p>', body_text: '完整正文' }
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

  it('finishes refresh against the current query and closes detail on scope changes', async () => {
    const { store, api } = setup()
    store.setQuery({ search: 'old' })
    await settle()
    await store.open(summary)
    const refresh = deferred<RefreshEmailsResponse>()
    api.refresh.mockReturnValueOnce(refresh.promise)
    const running = store.refresh()
    store.setQuery({ search: 'new', account_id: 2 })
    expect(store.getSnapshot().selectedEmail).toBeNull()
    refresh.resolve({ message: 'ok', new_count: 1 })
    await running
    expect(api.list.mock.calls.at(-1)?.[0]).toEqual({ search: 'new', account_id: 2 })
  })

  it('patches selected detail and its cache without replacing the body with a summary', async () => {
    const { store, api } = setup()
    await store.reload()
    await store.open(summary)
    expect(store.getSnapshot().selectedDetail?.body_html).toBe(detail.body_html)
    store.closeDetail()
    await store.open(summary)
    expect(store.getSnapshot().selectedDetail).toMatchObject({ body_html: detail.body_html })
    api.list.mockResolvedValue({ data: [{ ...summary, subject: 'Updated' }], total: 1 })
    await store.reload()
    expect(store.getSnapshot().selectedDetail).toMatchObject({ body_html: detail.body_html, subject: 'Updated' })
    expect(api.get).toHaveBeenCalledTimes(1)
  })

  it('deduplicates active detail requests, cancels unused ones and retries failures', async () => {
    const { store, api } = setup()
    const first = deferred<{ data: EmailDetail }>()
    api.get.mockReturnValueOnce(first.promise)
    const loading = store.open(summary)
    expect(store.open(summary)).toBe(loading)
    expect(api.get).toHaveBeenCalledTimes(1)
    store.closeDetail()
    expect(api.get.mock.calls[0][1]?.aborted).toBe(true)
    first.resolve({ data: detail })
    await loading
    expect(store.getSnapshot().selectedDetail).toBeNull()
    api.get.mockRejectedValueOnce(new Error('offline'))
    await store.open(summary)
    expect(store.getSnapshot().detailError).toContain('重试')
    expect(store.getSnapshot().selectedDetail).toBeNull()
    await store.retryDetail()
    expect(api.get).toHaveBeenCalledTimes(3)
    expect(store.getSnapshot().selectedDetail?.body_html).toBe(detail.body_html)
    expect(store.getSnapshot().detailError).toBeNull()
  })

  it('keeps the latest summary when a detail response arrives after refresh', async () => {
    const { store, api } = setup()
    const body = deferred<{ data: EmailDetail }>()
    api.get.mockReturnValueOnce(body.promise)
    await store.reload()
    const loading = store.open(summary)
    api.list.mockResolvedValue({ data: [{ ...summary, subject: 'Updated' }], total: 1 })
    await store.reload()
    body.resolve({ data: detail })
    await loading
    expect(store.getSnapshot().selectedDetail).toMatchObject({ subject: 'Updated', body_html: detail.body_html })
  })

  it('cancels detail for a message omitted by a refreshed account list', async () => {
    const { store, api } = setup()
    const body = deferred<{ data: EmailDetail }>()
    store.setQuery({ account_id: 1 })
    await settle()
    api.get.mockReturnValueOnce(body.promise)
    const loading = store.open(summary)
    api.list.mockResolvedValue({ data: [], total: 0 })
    await store.reload()
    expect(api.get.mock.calls[0][1]?.aborted).toBe(true)
    body.resolve({ data: detail })
    await loading
    expect(store.getSnapshot().selectedEmail).toBeNull()
    expect(store.getSnapshot().selectedDetail).toBeNull()
  })

  it('prunes only the refreshed account cache and preserves other account bodies', async () => {
    const { store, api } = setup()
    const second = { ...summary, id: 2, account_id: 2 }
    api.get.mockImplementation(async id => ({ data: { ...detail, ...(id === 2 ? second : summary) } }))
    await store.open(summary)
    await store.open(second)
    api.list.mockResolvedValue({ data: [], total: 0 })
    store.setQuery({ account_id: 1 })
    await settle()
    await store.open(second)
    expect(api.get).toHaveBeenCalledTimes(2)
    await store.open(summary)
    expect(api.get).toHaveBeenCalledTimes(3)
  })

  it('preserves cached bodies omitted by searches and combined latest-50 lists', async () => {
    const { store, api } = setup()
    await store.open(summary)
    api.list.mockResolvedValue({ data: [], total: 0 })
    store.setQuery({ search: 'different', account_id: 1 })
    await settle()
    await store.open(summary)
    store.setQuery({})
    await settle()
    await store.open(summary)
    expect(api.get).toHaveBeenCalledTimes(1)
  })

  it('keeps the previous list and exposes retry after a list failure', async () => {
    const { store, api } = setup()
    await store.reload()
    api.list.mockRejectedValueOnce(new Error('offline'))
    await store.reload()
    expect(store.getSnapshot().emails).toEqual([summary])
    expect(store.getSnapshot().listError).toContain('重试')
    await store.reload()
    expect(store.getSnapshot().listError).toBeNull()
  })

  it('reports a completed sync separately from a failed list read and retries only GET', async () => {
    const { store, api } = setup()
    await store.reload()
    api.list.mockRejectedValueOnce(new Error('list offline'))
    const result = await store.refresh()
    expect(result.listUpdated).toBe(false)
    expect(store.getSnapshot().emails).toEqual([summary])
    expect(store.getSnapshot().listError).toContain('重试')
    expect(await store.reload()).toBe('updated')
    expect(api.refresh).toHaveBeenCalledTimes(1)
    expect(store.getSnapshot().listError).toBeNull()
  })

  it('does not call a superseded list response a successful page update', async () => {
    const { store, api } = setup()
    const old = deferred<EmailListResponse>()
    api.list.mockReturnValueOnce(old.promise)
    const pending = store.refresh()
    await settle()
    store.setQuery({ search: 'current' })
    await settle()
    old.resolve({ data: [{ ...summary, subject: 'stale' }], total: 1 })
    expect((await pending).listUpdated).toBe(false)
    expect(store.getSnapshot().emails).toEqual([summary])
  })

  it('preserves partial sync errors when the following list GET also fails', async () => {
    const { store, api } = setup()
    api.refresh.mockResolvedValueOnce({ message: 'partial', new_count: 1, errors: { 'offline@example.com': 'timeout' } })
    api.list.mockRejectedValueOnce(new Error('list offline'))
    expect(await store.refresh()).toMatchObject({ listUpdated: false, new_count: 1, errors: { 'offline@example.com': 'timeout' } })
    expect(store.getSnapshot().listError).toContain('重试')
  })

  it('aborts requests on disposal and ignores late transport responses', async () => {
    const { store, api } = setup()
    const list = deferred<EmailListResponse>()
    const body = deferred<{ data: EmailDetail }>()
    api.list.mockReturnValueOnce(list.promise)
    api.get.mockReturnValueOnce(body.promise)
    const listing = store.reload()
    const opening = store.open(summary)
    const listener = vi.fn()
    store.subscribe(listener)
    store.dispose()
    expect(api.list.mock.calls[0][1]?.aborted).toBe(true)
    expect(api.get.mock.calls[0][1]?.aborted).toBe(true)
    list.resolve({ data: [summary], total: 1 })
    body.resolve({ data: detail })
    await Promise.all([listing, opening])
    expect(listener).not.toHaveBeenCalled()
    expect(store.getSnapshot().emails).toEqual([])
    expect(store.getSnapshot().selectedDetail).toBeNull()
  })

  it('ignores responses and prevents new requests immediately when the session expires', async () => {
    let valid = true
    const api = {
      ...emailApi,
      list: vi.fn<typeof emailApi.list>(),
      get: vi.fn<typeof emailApi.get>(),
    }
    const list = deferred<EmailListResponse>()
    const body = deferred<{ data: EmailDetail }>()
    api.list.mockReturnValue(list.promise)
    api.get.mockReturnValue(body.promise)
    const store = new MailStore(api, () => valid)
    stores.push(store)
    const listing = store.reload()
    const opening = store.open(summary)
    valid = false
    list.resolve({ data: [summary], total: 1 })
    body.resolve({ data: detail })
    await Promise.all([listing, opening])
    await store.reload()
    await store.open(summary)
    expect(api.list).toHaveBeenCalledTimes(1)
    expect(api.get).toHaveBeenCalledTimes(1)
    expect(store.getSnapshot().emails).toEqual([])
    expect(store.getSnapshot().selectedDetail).toBeNull()
  })

  it('evicts least recently accessed bodies when the byte budget is exhausted', async () => {
    const { api } = setup()
    api.get.mockImplementation(async id => ({ data: { ...detail, id, body_html: '', body_text: 'abcd' } }))
    const store = new MailStore(api, () => true, 16)
    stores.push(store)
    await store.open(summary)
    await store.open({ ...summary, id: 2 })
    await store.open(summary)
    await store.open({ ...summary, id: 3 })
    await store.open(summary)
    expect(api.get.mock.calls.map(([id]) => id)).toEqual([1, 2, 3])
    await store.open({ ...summary, id: 2 })
    expect(api.get.mock.calls.map(([id]) => id)).toEqual([1, 2, 3, 2])
  })

  it('shows oversized bodies without retaining them across reads', async () => {
    const { store, api } = setup()
    api.get.mockResolvedValue({ data: { ...detail, body_html: '', body_text: 'x'.repeat(1024 * 1024 + 1) } })
    await store.open(summary)
    expect(store.getSnapshot().selectedDetail?.body_text).toHaveLength(1024 * 1024 + 1)
    store.closeDetail()
    await store.open(summary)
    expect(api.get).toHaveBeenCalledTimes(2)
  })

  it('removes invalidated account bodies and ignores their pending detail', async () => {
    const { store, api } = setup()
    await store.reload()
    const pending = deferred<{ data: EmailDetail }>()
    api.get.mockReturnValueOnce(pending.promise)
    const opening = store.open(summary)
    store.invalidateAccount(summary.account_id)
    pending.resolve({ data: detail })
    await opening
    expect(store.getSnapshot().emails).toEqual([])
    expect(store.getSnapshot().selectedDetail).toBeNull()
    await store.open(summary)
    expect(api.get).toHaveBeenCalledTimes(2)
  })
})
