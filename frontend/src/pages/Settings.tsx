import { useState, useEffect, useRef } from 'react'
import { telegramApi } from '../api'
import { Save } from 'lucide-react'
import NotificationTasks from '../components/NotificationTasks'
import axios from 'axios'
import type { TelegramConfig } from '../api'
import useReadingPreferences from '../context/useReadingPreferences'
import { DEFAULT_EMAIL_LIMIT, EMAIL_LIMIT_OPTIONS, type EmailLimit } from '../mail/limits'

export default function Settings() {
  const { blockRemoteImages, setBlockRemoteImages, emailLimit, setEmailLimit } = useReadingPreferences()
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
    <div className="workspace-page settings-page">
      {toast && (
        <div className="fixed left-4 right-4 top-4 z-50 md:left-auto md:right-4">
          <div role="status"
            className={`flex items-center gap-3 px-4 py-3 rounded-[8px] text-sm border shadow-[var(--shadow-md)] ${
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
      <div className="page-heading">
        <h1>设置</h1>
        <p>
          管理邮件阅读偏好和 Telegram 推送
        </p>
      </div>

      <section aria-labelledby="reading-settings-title" className="settings-panel">
        <div className="panel-heading">
          <h2 id="reading-settings-title" className="text-base font-medium text-[var(--text-primary)]">邮件阅读</h2>
          <p className="mt-1 text-sm text-[var(--text-secondary)]">偏好会自动保存到当前浏览器</p>
        </div>
        <div className="setting-row setting-row-select">
          <div className="min-w-0">
            <label htmlFor="email-limit" className="block text-sm font-medium text-[var(--text-primary)]">默认加载邮件数</label>
            <p id="email-limit-help" className="mt-1 text-sm text-[var(--text-secondary)]">列表和手动刷新使用此数量，调大后点击刷新可拉取更多邮件</p>
          </div>
          <select
            id="email-limit"
            aria-describedby="email-limit-help"
            value={emailLimit}
            onChange={event => setEmailLimit(Number(event.target.value) as EmailLimit)}
            className="w-full sm:w-32 flex-shrink-0"
          >
            {EMAIL_LIMIT_OPTIONS.map(limit => <option key={limit} value={limit}>{limit} 封{limit === DEFAULT_EMAIL_LIMIT ? '（默认）' : ''}</option>)}
          </select>
        </div>
        <div className="setting-row">
          <div className="min-w-0">
            <span className="block text-sm font-medium text-[var(--text-primary)]">阻止远程图片</span>
            <p id="remote-images-help" className="mt-1 text-sm text-[var(--text-secondary)]">默认显示远程图片；开启后阻止自动加载，减少邮件跟踪，仍可逐封临时加载</p>
          </div>
          <button
            type="button"
            role="switch"
            aria-label="阻止远程图片"
            aria-describedby="remote-images-help"
            aria-checked={blockRemoteImages}
            onClick={() => setBlockRemoteImages(!blockRemoteImages)}
            className="btn btn-ghost relative h-9 w-11 flex-shrink-0 p-0 rounded-lg"
          >
            <span
              aria-hidden="true"
              className={`absolute inset-x-0 top-1/2 -translate-y-1/2 h-6 rounded-full transition-colors ${
                blockRemoteImages ? 'bg-[var(--primary-500)]' : 'bg-[var(--bg-tertiary)] shadow-[inset_0_0_0_1px_var(--border-default)]'
              }`}
            >
              <span className={`toggle-thumb absolute left-0.5 top-0.5 h-5 w-5 rounded-full transition-transform ${blockRemoteImages ? 'translate-x-5' : 'translate-x-0'}`} />
            </span>
          </button>
        </div>
      </section>

        <section aria-labelledby="telegram-settings-title" className="settings-panel">
          <div className="panel-heading">
            <h2 id="telegram-settings-title" className="text-base font-medium text-[var(--text-primary)]">Telegram 通知</h2>
            <p className="mt-1 text-sm text-[var(--text-secondary)]">手动刷新发现新邮件后，将摘要推送到指定会话，临时失败会自动重试</p>
          </div>

          {telegramError && (
            <div role="alert" className="alert alert-error mb-4">{telegramError}</div>
          )}
          {configLoading && <p role="status" className="mb-4 text-sm text-[var(--text-secondary)]">加载 Telegram 配置中...</p>}
          {!configLoading && !configLoaded && (
            <button type="button" className="btn btn-secondary mb-4" onClick={() => { setConfigLoading(true); setTelegramError(''); setLoadAttempt(attempt => attempt + 1) }}>重新加载</button>
          )}

          <form onSubmit={handleTelegramSubmit} className="space-y-6">
            <fieldset disabled={configLoading || !configLoaded || telegramLoading || telegramTesting} className="min-w-0 space-y-6">
            <div className="setting-row pb-5 border-b border-[var(--border-light)]">
              <div>
                <span className="block text-sm font-medium text-[var(--text-primary)]">启用自动推送</span>
                <span id="telegram-enabled-help" className="block text-sm text-[var(--text-secondary)] mt-1">
                  刷新发现新邮件后自动转发摘要到 Telegram
                </span>
              </div>
              <button
                type="button"
                role="switch"
                aria-label="启用自动推送"
                aria-describedby="telegram-enabled-help"
                aria-checked={telegramConfig.enabled}
                onClick={() => setTelegramConfig({ ...telegramConfig, enabled: !telegramConfig.enabled })}
                className="btn btn-ghost relative h-9 w-11 flex-shrink-0 p-0 rounded-lg"
              >
                <span
                  aria-hidden="true"
                  className={`absolute inset-x-0 top-1/2 -translate-y-1/2 h-6 rounded-full transition-colors ${
                    telegramConfig.enabled ? 'bg-[var(--primary-500)]' : 'bg-[var(--bg-tertiary)] shadow-[inset_0_0_0_1px_var(--border-default)]'
                  }`}
                >
                  <span
                    className={`toggle-thumb absolute left-0.5 top-0.5 h-5 w-5 rounded-full transition-transform ${
                      telegramConfig.enabled ? 'translate-x-5' : 'translate-x-0'
                    }`}
                  />
                </span>
              </button>
            </div>

            <div>
              <label htmlFor="telegram-bot-token" className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                Bot Token
              </label>
              <input
                id="telegram-bot-token"
                type="text"
                aria-describedby="telegram-bot-token-help"
                value={telegramConfig.bot_token}
                onChange={(e) => setTelegramConfig({ ...telegramConfig, bot_token: e.target.value })}
                className="font-mono text-sm"
                placeholder="123456789:ABCdefGHIjklMNOpqrsTUVwxyz"
              />
              <p id="telegram-bot-token-help" className="mt-2 text-xs text-[var(--text-tertiary)]">从 @BotFather 创建机器人后获取</p>
            </div>

            <div>
              <label htmlFor="telegram-chat-id" className="block text-sm font-medium text-[var(--text-secondary)] mb-2">
                Chat ID
              </label>
              <input
                id="telegram-chat-id"
                type="text"
                aria-describedby="telegram-chat-id-help"
                value={telegramConfig.chat_id}
                onChange={(e) => setTelegramConfig({ ...telegramConfig, chat_id: e.target.value })}
                className="font-mono text-sm"
                placeholder="-1001234567890"
              />
              <p id="telegram-chat-id-help" className="mt-2 text-xs text-[var(--text-tertiary)]">支持个人用户 ID、群组 ID 或频道 ID</p>
            </div>

            <div className="flex flex-col-reverse gap-2 pt-4 border-t border-[var(--border-light)] sm:flex-row sm:justify-end">
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
        <NotificationTasks />
    </div>
  )
}
