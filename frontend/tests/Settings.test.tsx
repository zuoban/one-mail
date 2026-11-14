import { act, fireEvent, render, screen } from '@testing-library/react'
import { AxiosError, AxiosHeaders } from 'axios'
import { describe, expect, it, vi } from 'vitest'
import { telegramApi, type TelegramConfig } from '../src/api'
import Settings from '../src/pages/Settings'

const initialConfig: TelegramConfig = { enabled: false, bot_token: 'bot-token', chat_id: 'chat-id' }

describe('Telegram settings', () => {
  it('loads the configuration before enabling edits and preserves save and test actions', async () => {
    const config = Promise.withResolvers<{ data: TelegramConfig }>()
    vi.spyOn(telegramApi, 'getConfig').mockReturnValue(config.promise)
    const save = vi.spyOn(telegramApi, 'updateConfig').mockResolvedValue({ data: { ...initialConfig, enabled: true, chat_id: 'new-chat' } })
    const test = vi.spyOn(telegramApi, 'testConnection').mockResolvedValue({ message: 'sent' })
    render(<Settings />)
    expect(screen.getByRole('status').textContent).toContain('加载 Telegram 配置')
    expect((screen.getByRole('button', { name: '保存配置' }) as HTMLButtonElement).disabled || screen.getByRole('button', { name: '保存配置' }).closest('fieldset')?.disabled).toBe(true)
    await act(async () => config.resolve({ data: initialConfig }))
    expect((screen.getByLabelText('Bot Token') as HTMLInputElement).value).toBe('bot-token')
    fireEvent.click(screen.getByRole('switch', { name: '启用自动推送' }))
    fireEvent.change(screen.getByLabelText('Chat ID'), { target: { value: 'new-chat' } })
    fireEvent.click(screen.getByRole('button', { name: '发送测试消息' }))
    expect((await screen.findByRole('status')).textContent).toContain('测试消息已发送')
    expect(test).toHaveBeenCalledWith({ bot_token: 'bot-token', chat_id: 'new-chat' })
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }))
    expect((await screen.findByText('Telegram 配置已保存')).textContent).toBe('Telegram 配置已保存')
    expect(save).toHaveBeenCalledWith({ enabled: true, bot_token: 'bot-token', chat_id: 'new-chat' })
    expect(screen.queryByRole('tab')).toBeNull()
  })

  it('reports a configuration load failure and retries without allowing an empty configuration to overwrite it', async () => {
    const load = vi.spyOn(telegramApi, 'getConfig').mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ data: initialConfig })
    const save = vi.spyOn(telegramApi, 'updateConfig')
    render(<Settings />)
    expect((await screen.findByRole('alert')).textContent).toContain('无法加载 Telegram 配置')
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }))
    expect(save).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '重新加载' }))
    await screen.findByDisplayValue('bot-token')
    expect(load).toHaveBeenCalledTimes(2)
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('keeps configuration edits and displays save and delivery errors', async () => {
    vi.spyOn(telegramApi, 'getConfig').mockResolvedValue({ data: initialConfig })
    vi.spyOn(telegramApi, 'updateConfig').mockRejectedValue(new Error('offline'))
    vi.spyOn(telegramApi, 'testConnection').mockRejectedValue(new AxiosError('bad chat', 'ERR_BAD_REQUEST', undefined, undefined, { data: { error: '无法发送到该会话' }, status: 400, statusText: 'Bad Request', headers: new AxiosHeaders(), config: { headers: new AxiosHeaders() } }))
    render(<Settings />)
    await screen.findByDisplayValue('bot-token')
    fireEvent.change(screen.getByLabelText('Chat ID'), { target: { value: 'invalid-chat' } })
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }))
    expect((await screen.findByRole('alert')).textContent).toBe('保存失败')
    expect((screen.getByLabelText('Chat ID') as HTMLInputElement).value).toBe('invalid-chat')
    fireEvent.click(screen.getByRole('button', { name: '发送测试消息' }))
    expect((await screen.findByText('无法发送到该会话')).textContent).toBe('无法发送到该会话')
  })
})
