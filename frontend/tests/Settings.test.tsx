import { act, fireEvent, render, screen } from '@testing-library/react'
import { AxiosError, AxiosHeaders } from 'axios'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { telegramApi, type TelegramConfig } from '../src/api'
import Settings from '../src/pages/Settings'
import EmailBody from '../src/components/EmailBody'
import { ReadingPreferencesProvider } from '../src/context/ReadingPreferencesContext'

const initialConfig: TelegramConfig = { enabled: false, bot_token: 'bot-token', chat_id: 'chat-id' }

beforeEach(() => {
  vi.spyOn(telegramApi, 'listTasks').mockResolvedValue({ data: [], pending_count: 0, failed_count: 0 })
})

describe('Telegram settings', () => {
  it('loads the configuration before enabling edits and preserves save and test actions', async () => {
    const config = Promise.withResolvers<{ data: TelegramConfig }>()
    vi.spyOn(telegramApi, 'getConfig').mockReturnValue(config.promise)
    const save = vi.spyOn(telegramApi, 'updateConfig').mockResolvedValue({ data: { ...initialConfig, enabled: true, chat_id: 'new-chat' } })
    const test = vi.spyOn(telegramApi, 'testConnection').mockResolvedValue({ message: 'sent' })
    render(<Settings />, { wrapper: ReadingPreferencesProvider })
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
    render(<Settings />, { wrapper: ReadingPreferencesProvider })
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
    render(<Settings />, { wrapper: ReadingPreferencesProvider })
    await screen.findByDisplayValue('bot-token')
    fireEvent.change(screen.getByLabelText('Chat ID'), { target: { value: 'invalid-chat' } })
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }))
    expect((await screen.findByRole('alert')).textContent).toBe('保存失败')
    expect((screen.getByLabelText('Chat ID') as HTMLInputElement).value).toBe('invalid-chat')
    fireEvent.click(screen.getByRole('button', { name: '发送测试消息' }))
    expect((await screen.findByText('无法发送到该会话')).textContent).toBe('无法发送到该会话')
  })
})

describe('mail reading preferences', () => {
  it('applies image blocking independently of Telegram loading and restores both choices after remount', () => {
    const config = Promise.withResolvers<{ data: TelegramConfig }>()
    vi.spyOn(telegramApi, 'getConfig').mockReturnValue(config.promise)
    const imageUrl = 'https://images.example/newsletter.png'
    const readingScreen = <><Settings /><EmailBody html={`<img src="${imageUrl}" alt="Newsletter">`} light /></>
    const imageSource = () => new DOMParser().parseFromString(screen.getByTitle<HTMLIFrameElement>('邮件正文').srcdoc, 'text/html').querySelector('img')?.getAttribute('src')
    const first = render(readingScreen, { wrapper: ReadingPreferencesProvider })
    const toggle = screen.getByRole('switch', { name: '阻止远程图片' })
    expect(toggle.getAttribute('aria-checked')).toBe('false')
    expect(imageSource()).toBe(imageUrl)
    fireEvent.click(toggle)
    expect(toggle.getAttribute('aria-checked')).toBe('true')
    expect(imageSource()).toBeNull()
    first.unmount()
    const second = render(readingScreen, { wrapper: ReadingPreferencesProvider })
    const restored = screen.getByRole('switch', { name: '阻止远程图片' })
    expect(restored.getAttribute('aria-checked')).toBe('true')
    expect(imageSource()).toBeNull()
    fireEvent.click(restored)
    expect(imageSource()).toBe(imageUrl)
    second.unmount()
    render(readingScreen, { wrapper: ReadingPreferencesProvider })
    expect(screen.getByRole('switch', { name: '阻止远程图片' }).getAttribute('aria-checked')).toBe('false')
    expect(imageSource()).toBe(imageUrl)
  })

  it('defaults to fifty messages and remembers another count without changing image privacy', () => {
    localStorage.setItem('blockRemoteImages', 'true')
    const config = Promise.withResolvers<{ data: TelegramConfig }>()
    vi.spyOn(telegramApi, 'getConfig').mockReturnValue(config.promise)
    const first = render(<Settings />, { wrapper: ReadingPreferencesProvider })
    const count = screen.getByRole('combobox', { name: '默认加载邮件数' }) as HTMLSelectElement
    expect(count.value).toBe('50')
    fireEvent.change(count, { target: { value: '100' } })
    first.unmount()
    render(<Settings />, { wrapper: ReadingPreferencesProvider })
    expect((screen.getByRole('combobox', { name: '默认加载邮件数' }) as HTMLSelectElement).value).toBe('100')
    expect(screen.getByRole('switch', { name: '阻止远程图片' }).getAttribute('aria-checked')).toBe('true')
  })
})
