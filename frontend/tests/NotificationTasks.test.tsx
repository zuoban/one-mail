import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import NotificationTasks from '../src/components/NotificationTasks'
import { telegramApi, type NotificationList } from '../src/api'

const failed: NotificationList = {
  data: [{
    id: 1, account_id: 2, account_email: 'owner@example.com', created_at: '2026-11-15T00:00:00Z',
    attempts: 1, next_attempt_at: '2026-11-15T01:00:00Z', status: 'failed', last_error: 'permanent: telegram message rejected',
  }],
  pending_count: 2,
  failed_count: 1,
}

it('shows failure counts and retries without duplicating pending actions', async () => {
  const load = vi.spyOn(telegramApi, 'listTasks').mockResolvedValueOnce(failed).mockResolvedValueOnce({ data: [], pending_count: 3, failed_count: 0 })
  const pending = Promise.withResolvers<Awaited<ReturnType<typeof telegramApi.retryTask>>>()
  const retry = vi.spyOn(telegramApi, 'retryTask').mockReturnValue(pending.promise)
  render(<NotificationTasks />)
  const button = await screen.findByRole('button', { name: '重试通知' })
  expect(screen.getByText('待投递 2 条 · 失败 1 条')).toBeTruthy()
  expect(screen.getByText('telegram message rejected')).toBeTruthy()
  fireEvent.click(button)
  fireEvent.click(button)
  expect(retry).toHaveBeenCalledExactlyOnceWith(1)
  expect((screen.getByRole('button', { name: '清理记录' }) as HTMLButtonElement).disabled).toBe(true)
  await act(async () => pending.resolve({} as Awaited<ReturnType<typeof telegramApi.retryTask>>))
  await screen.findByText('没有失败的通知任务')
  expect(load).toHaveBeenCalledTimes(2)
})

it('retains failed tasks after a mutation error and allows deletion', async () => {
  vi.spyOn(telegramApi, 'listTasks').mockResolvedValueOnce(failed).mockResolvedValueOnce({ data: [], pending_count: 2, failed_count: 0 })
  vi.spyOn(telegramApi, 'retryTask').mockRejectedValue(new Error('offline'))
  const remove = vi.spyOn(telegramApi, 'deleteTask').mockResolvedValue({} as Awaited<ReturnType<typeof telegramApi.deleteTask>>)
  render(<NotificationTasks />)
  fireEvent.click(await screen.findByRole('button', { name: '重试通知' }))
  expect(await screen.findByText('重新投递失败，请重试')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '清理记录' }))
  await screen.findByText('没有失败的通知任务')
  expect(remove).toHaveBeenCalledExactlyOnceWith(1)
})

it('retries failed reads and cancels a read after leaving settings', async () => {
  const pending = Promise.withResolvers<NotificationList>()
  const load = vi.spyOn(telegramApi, 'listTasks').mockRejectedValueOnce(new Error('offline')).mockReturnValueOnce(pending.promise)
  const view = render(<NotificationTasks />)
  await screen.findByText('投递记录加载失败，请重试')
  fireEvent.click(screen.getByRole('button', { name: '刷新投递记录' }))
  await waitFor(() => expect(load).toHaveBeenCalledTimes(2))
  const signal = load.mock.calls[1][0]!
  view.unmount()
  expect(signal.aborted).toBe(true)
  await act(async () => pending.resolve(failed))
  expect(screen.queryByText('owner@example.com')).toBeNull()
})
