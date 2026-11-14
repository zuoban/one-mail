import { expect, it, vi } from 'vitest'
import type { AxiosAdapter } from 'axios'
import api, { accountApi, emailApi } from '../src/api'

it('forwards account list cancellation and preserves the 45-second refresh timeout', async () => {
  const previousAdapter = api.defaults.adapter
  const adapter = vi.fn<AxiosAdapter>(async config => ({ data: config.url === '/accounts' ? { data: [] } : { message: 'ok', new_count: 0 }, status: 200, statusText: 'OK', headers: {}, config }))
  api.defaults.adapter = adapter
  try {
    const controller = new AbortController()
    expect(await accountApi.list(controller.signal)).toEqual({ data: [] })
    await accountApi.refresh(1)
    await emailApi.refresh()
    expect(adapter.mock.calls[0][0].signal).toBe(controller.signal)
    expect(adapter.mock.calls.slice(1).map(([config]) => config.timeout)).toEqual([45000, 45000])
  } finally {
    api.defaults.adapter = previousAdapter
  }
})
