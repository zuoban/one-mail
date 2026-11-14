import { useState, useEffect, useRef } from 'react'
import { telegramApi } from '../api'
import { Save } from 'lucide-react'
import axios from 'axios'
import type { TelegramConfig } from '../api'

export default function Settings() {
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null)

  const [telegramConfig, setTelegramConfig] = useState<TelegramConfig>({
    enabled: false,
    bot_token: '',
    chat_id: '',
  })
  const [telegramLoading, setTelegramLoading] = useState(false)
  const [telegramTesting, setTelegramTesting] = useState(false)
  const [telegramError, setTelegramError] = useState('')
  const [configLoading, setConfigLoading] = useState(true)
  const [configLoaded, setConfigLoaded] = useState(false)
  const [loadAttempt, setLoadAttempt] = useState(0)
  const toastTimer = useRef<number | undefined>(undefined)

  useEffect(() => {
    let active = true
    telegramApi.getConfig().then(res => {
      if (!active) return
      setTelegramConfig(res.data)
      setConfigLoaded(true)
    }).catch(() => {
      if (active) setTelegramError('无法加载 Telegram 配置，请重试')
    }).finally(() => {
      if (active) setConfigLoading(false)
    })
    return () => { active = false }
  }, [loadAttempt])

  useEffect(() => () => window.clearTimeout(toastTimer.current), [])

  const showToast = (message: string, type: 'success' | 'error') => {
    setToast({ message, type })
    window.clearTimeout(toastTimer.current)
    toastTimer.current = window.setTimeout(() => setToast(null), 2000)
  }

  const handleTelegramSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setTelegramError('')
    setTelegramLoading(true)
    try {
      const res = await telegramApi.updateConfig({
        enabled: telegramConfig.enabled,
        bot_token: telegramConfig.bot_token,
        chat_id: telegramConfig.chat_id,
      })
      setTelegramConfig(res.data)
      showToast('Telegram 配置已保存', 'success')
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setTelegramError(message || '保存失败')
    } finally {
      setTelegramLoading(false)
    }
  }

  const handleTelegramTest = async () => {
    setTelegramError('')
    setTelegramTesting(true)
    try {
      await telegramApi.testConnection({
        bot_token: telegramConfig.bot_token,
        chat_id: telegramConfig.chat_id,
      })
      showToast('测试消息已发送', 'success')
    } catch (err: unknown) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setTelegramError(message || '发送测试消息失败')
    } finally {
      setTelegramTesting(false)
    }
  }

  return (
    <div className="max-w-3xl mx-auto p-4 md:p-8">
      {toast && (
        <div className="fixed left-4 right-4 top-4 z-50 md:left-auto md:right-4">
          <div role="status"
            className={`flex items-center gap-2.5 px-4 py-2.5 rounded-[8px] text-sm border shadow-[var(--shadow-md)] ${
              toast.type === 'success'
                ? 'bg-[var(--bg-primary)] text-[var(--text-primary)] border-[var(--border-default)]'
                : 'bg-[var(--error-50)] text-[var(--error-700)] border-[var(--error-100)]'
            }`}
          >
            <span className={`status-dot ${toast.type === 'success' ? 'status-dot-success' : 'status-dot-error'}`} />
            <span>{toast.message}</span>
          </div>
        </div>
      )}

      {/* Header */}
      <div className="mb-6">
        <h1 className="text-2xl font-medium tracking-[-0.015em] text-[var(--text-primary)]">设置</h1>
        <p className="mt-1 text-sm text-[var(--text-secondary)]">
          管理 Telegram 邮件推送
        </p>
      </div>

        <section className="rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] p-5 md:p-6 shadow-[var(--shadow-sm)]">
          <div className="pb-4 mb-5 border-b border-[var(--border-light)]">
            <h2 className="text-base font-medium text-[var(--text-primary)]">Telegram 通知</h2>
            <p className="mt-0.5 text-sm text-[var(--text-secondary)]">连接 Bot 后可将新邮件推送到指定会话</p>
          </div>

          {telegramError && (
            <div role="alert" className="alert alert-error mb-4">{telegramError}</div>
          )}
          {configLoading && <p role="status" className="mb-4 text-sm text-[var(--text-secondary)]">加载 Telegram 配置中...</p>}
          {!configLoading && !configLoaded && (
            <button type="button" className="btn btn-secondary mb-4" onClick={() => { setConfigLoading(true); setTelegramError(''); setLoadAttempt(attempt => attempt + 1) }}>重新加载</button>
          )}

          <form onSubmit={handleTelegramSubmit} className="space-y-4">
            <fieldset disabled={configLoading || !configLoaded || telegramLoading || telegramTesting} className="space-y-4">
            <div className="flex items-center justify-between gap-4 p-4 rounded-[10px] bg-[var(--bg-secondary)] border border-[var(--border-light)]">
              <div>
                <span className="block text-sm font-medium text-[var(--text-primary)]">启用自动推送</span>
                <span className="block text-xs text-[var(--text-tertiary)] mt-0.5">
                  收到新邮件时自动转发摘要到 Telegram
                </span>
              </div>
              <button
                type="button"
                role="switch"
                aria-label="启用自动推送"
                aria-checked={telegramConfig.enabled}
                onClick={() => setTelegramConfig({ ...telegramConfig, enabled: !telegramConfig.enabled })}
                className={`relative h-6 w-11 rounded-full transition-colors cursor-pointer ${
                  telegramConfig.enabled ? 'bg-[var(--primary-500)]' : 'bg-[var(--bg-tertiary)] shadow-[inset_0_0_0_1px_var(--border-default)]'
                }`}
              >
                <span
                  className={`toggle-thumb absolute left-0.5 top-0.5 h-5 w-5 rounded-full transition-transform ${
                    telegramConfig.enabled ? 'translate-x-5' : 'translate-x-0'
                  }`}
                />
              </button>
            </div>

            <div>
              <label htmlFor="telegram-bot-token" className="block text-sm font-medium text-[var(--text-secondary)] mb-1.5">
                Bot Token
              </label>
              <input
                id="telegram-bot-token"
                type="text"
                value={telegramConfig.bot_token}
                onChange={(e) => setTelegramConfig({ ...telegramConfig, bot_token: e.target.value })}
                className="font-mono text-sm"
                placeholder="123456789:ABCdefGHIjklMNOpqrsTUVwxyz"
              />
              <p className="mt-1.5 text-xs text-[var(--text-tertiary)]">从 @BotFather 创建机器人后获取</p>
            </div>

            <div>
              <label htmlFor="telegram-chat-id" className="block text-sm font-medium text-[var(--text-secondary)] mb-1.5">
                Chat ID
              </label>
              <input
                id="telegram-chat-id"
                type="text"
                value={telegramConfig.chat_id}
                onChange={(e) => setTelegramConfig({ ...telegramConfig, chat_id: e.target.value })}
                className="font-mono text-sm"
                placeholder="-1001234567890"
              />
              <p className="mt-1.5 text-xs text-[var(--text-tertiary)]">支持个人用户 ID、群组 ID 或频道 ID</p>
            </div>

            <div className="flex flex-col-reverse gap-2 pt-2 sm:flex-row sm:justify-between">
              <button
                type="button"
                onClick={handleTelegramTest}
                disabled={telegramTesting || !telegramConfig.bot_token || !telegramConfig.chat_id}
                className="btn btn-secondary w-full sm:w-auto"
              >
                {telegramTesting ? '发送中' : '发送测试消息'}
              </button>
              <button
                type="submit"
                disabled={telegramLoading}
                className="btn btn-primary w-full sm:w-auto"
              >
                <Save className="w-4 h-4" strokeWidth={1.5} />
                <span>{telegramLoading ? '保存中' : '保存配置'}</span>
              </button>
            </div>
            </fieldset>
          </form>
        </section>
    </div>
  )
}
