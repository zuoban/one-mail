import { beforeEach, expect, it, vi } from 'vitest'
import { AxiosHeaders } from 'axios'
import { accountApi, providerApi, type AccountInput, type EmailAccount } from '../src/api'
import { AccountStore } from '../src/accounts/AccountStore'

const account: EmailAccount = { id: 1, email: 'reader@example.com', provider: 'custom', username: 'reader', status: 'active', last_sync_time: '', imap_host: 'imap.example.com', imap_port: 993 }

beforeEach(() => {
  vi.spyOn(accountApi, 'list').mockResolvedValue({ data: [account] })
  vi.spyOn(accountApi, 'update').mockResolvedValue({ data: account })
})

it.each<Partial<AccountInput>>([
  { email: 'changed@example.com' },
  { provider: 'gmail' },
  { username: 'changed-user' },
  { imap_host: 'moved.example.com' },
  { imap_port: 1993 },
])('invalidates mail after a successful identity update: %o', async input => {
  const invalidateMail = vi.fn()
  const store = new AccountStore(() => true, invalidateMail)
  await store.reload()
  await store.update(account.id, input)
  expect(invalidateMail).toHaveBeenCalledExactlyOnceWith(account.id)
  store.dispose()
})

it('retains mail for password and unchanged IMAP settings, and invalidates it after deletion', async () => {
  const invalidateMail = vi.fn()
  const store = new AccountStore(() => true, invalidateMail)
  await store.reload()
  await store.update(account.id, { password: 'new-password', imap_host: account.imap_host, imap_port: account.imap_port })
  expect(invalidateMail).not.toHaveBeenCalled()
  vi.spyOn(accountApi, 'delete').mockResolvedValue({ data: {}, status: 200, statusText: 'OK', headers: {}, config: { headers: new AxiosHeaders() } })
  await store.remove(account.id)
  expect(invalidateMail).toHaveBeenCalledExactlyOnceWith(account.id)
  store.dispose()
})

it('keeps mail when an identity update fails', async () => {
  const invalidateMail = vi.fn()
  const store = new AccountStore(() => true, invalidateMail)
  await store.reload()
  vi.mocked(accountApi.update).mockRejectedValueOnce(new Error('offline'))
  await expect(store.update(account.id, { imap_host: 'uncommitted.example.com' })).rejects.toThrow('offline')
  expect(invalidateMail).not.toHaveBeenCalled()
  store.dispose()
})

it('restarts an interrupted reload when the next route subscribes', async () => {
  vi.spyOn(providerApi, 'list').mockResolvedValue({ data: [] })
  const store = new AccountStore()
  const leave = store.subscribe(vi.fn())
  await store.reload()
  const interrupted = Promise.withResolvers<{ data: EmailAccount[] }>()
  const changed = { ...account, email: 'changed@example.com' }
  vi.mocked(accountApi.list).mockReturnValueOnce(interrupted.promise).mockResolvedValueOnce({ data: [changed] })
  const retry = store.reload()
  leave()
  expect(vi.mocked(accountApi.list).mock.calls[1][0]?.aborted).toBe(true)
  const unsubscribe = store.subscribe(vi.fn())
  store.ensure()
  interrupted.resolve({ data: [account] })
  await retry
  expect(store.getSnapshot().accounts).toEqual([changed])
  expect(store.getSnapshot().loadingAccounts).toBe(false)
  expect(accountApi.list).toHaveBeenCalledTimes(3)
  unsubscribe()
  store.dispose()
})
