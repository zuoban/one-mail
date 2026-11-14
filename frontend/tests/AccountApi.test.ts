import { expect, it, vi } from 'vitest'
import type { AxiosAdapter } from 'axios'
import api, { accountApi, emailApi, providerApi } from '../src/api'

it('forwards account cancellation, configured message counts and the 45-second refresh timeout', async () => {
  const previousAdapter = api.defaults.adapter
  const adapter = vi.fn<AxiosAdapter>(async config => ({ data: config.url === '/accounts' ? { data: [] } : { message: 'ok', new_count: 0 }, status: 200, statusText: 'OK', headers: {}, config }))
  api.defaults.adapter = adapter
  try {
    const controller = new AbortController()
    expect(await accountApi.list(controller.signal)).toEqual({ data: [] })
    await accountApi.refresh(1, 100)
    await emailApi.refresh(2, 100)
    expect(adapter.mock.calls[0][0].signal).toBe(controller.signal)
    expect(adapter.mock.calls.slice(1).map(([config]) => config.timeout)).toEqual([45000, 45000])
    expect(adapter.mock.calls[1][0].params).toEqual({ limit: 100 })
    expect(adapter.mock.calls[2][0].params).toEqual({ account_id: 2, limit: 100 })
  } finally {
    api.defaults.adapter = previousAdapter
  }
})

it('reads backend provider metadata with cancellation and sends custom IMAP fields unchanged', async () => {
  const previousAdapter = api.defaults.adapter
  const providers = [{ id: 'private', label: '企业邮箱', custom: true, imap_host: '', imap_port: 993 }]
  const adapter = vi.fn<AxiosAdapter>(async config => ({ data: config.url === '/providers' ? { data: providers } : { data: {}, refresh_error: '同步未完成' }, status: 200, statusText: 'OK', headers: {}, config }))
  api.defaults.adapter = adapter
  try {
    const controller = new AbortController()
    expect(await providerApi.list(controller.signal)).toEqual({ data: providers })
    expect(adapter.mock.calls[0][0].signal).toBe(controller.signal)
    const input = { email: 'reader@example.com', username: 'reader', provider: 'private', password: 'password', imap_host: 'imap.private.example', imap_port: 1993 }
    expect((await accountApi.add(input)).refresh_error).toBe('同步未完成')
    expect(adapter.mock.calls[1][0].timeout).toBe(35000)
    await accountApi.update(1, { imap_host: 'moved.private.example', imap_port: 2993 })
    expect(JSON.parse(adapter.mock.calls[1][0].data)).toEqual(input)
    expect(JSON.parse(adapter.mock.calls[2][0].data)).toEqual({ imap_host: 'moved.private.example', imap_port: 2993 })
  } finally {
    api.defaults.adapter = previousAdapter
  }
})
