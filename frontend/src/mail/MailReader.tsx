import { useState, type RefObject } from 'react'
import { ArrowLeft, ChevronUp, ChevronDown, Copy, Loader2, Mail, Moon, Paperclip, Send, Sun } from 'lucide-react'
import type { EmailAccount, EmailDetail, EmailSummary } from '../api'
import Tooltip from '../components/Tooltip'
import EmailBody from '../components/EmailBody'
import useTheme from '../context/useTheme'

interface Props {
  selectedEmail: EmailSummary | null
  selectedDetail: EmailDetail | null
  selectedEmailAccount?: EmailAccount
  selectedIndex: number
  total: number
  loadingDetail: boolean
  detailError: string | null
  sendingTelegram: boolean
  detailScrollRef: RefObject<HTMLDivElement | null>
  stepEmail: (delta: number) => void
  closeDetail: () => void
  retryDetail: () => Promise<unknown>
  handleSendToTelegram: (email: EmailSummary) => Promise<void>
  showToast: (message: string, type: 'success' | 'error') => void
}

export default function MailReader({ selectedEmail, selectedDetail, selectedEmailAccount, selectedIndex, total, loadingDetail, detailError, sendingTelegram, detailScrollRef, stepEmail, closeDetail, retryDetail, handleSendToTelegram, showToast }: Props) {
  const { resolvedTheme } = useTheme()
  const [paperLight, setPaperLight] = useState(true)
  const useLightPaper = Boolean(selectedDetail?.body_html) && (resolvedTheme === 'light' || paperLight)
  return (
<section aria-label="邮件阅读区" className="mail-reader flex-1 min-w-0 bg-[var(--bg-secondary)] min-h-0 flex-col">
        {selectedEmail ? (
          <div className="h-full flex flex-col">
            {/* Action Toolbar */}
            <div className="reading-toolbar h-16 px-4 xl:px-6 border-b border-[var(--border-light)] bg-[var(--bg-primary)] flex items-center justify-between gap-2 flex-shrink-0">
              <div className="flex items-center gap-2 min-w-0">
                <button
                  type="button"
                  onClick={closeDetail}
                  className="lg:hidden btn btn-ghost btn-sm px-2"
                >
                  <ArrowLeft className="w-4 h-4" strokeWidth={1.5} />
                  <span>返回</span>
                </button>

                <div className="hidden lg:flex items-center gap-1 pr-2 border-r border-[var(--border-light)]">
                  <Tooltip content="上一封 (k)">
                    <button
                      type="button"
                      onClick={() => stepEmail(-1)}
                      disabled={selectedIndex <= 0}
                      className="btn btn-ghost w-7 h-7 p-0"
                      aria-label="上一封"
                    >
                      <ChevronUp className="w-4 h-4" strokeWidth={1.5} />
                    </button>
                  </Tooltip>
                  <Tooltip content="下一封 (j)">
                    <button
                      type="button"
                      onClick={() => stepEmail(1)}
                      disabled={selectedIndex < 0 || selectedIndex >= total - 1}
                      className="btn btn-ghost w-7 h-7 p-0"
                      aria-label="下一封"
                    >
                      <ChevronDown className="w-4 h-4" strokeWidth={1.5} />
                    </button>
                  </Tooltip>
                  {selectedIndex >= 0 && (
                    <span className="font-mono tabular-nums text-xs text-[var(--text-tertiary)] pl-1">
                      {selectedIndex + 1}/{total}
                    </span>
                  )}
                </div>

                {selectedEmail.folder && (
                  <span className="hidden xl:inline-flex px-2 py-0.5 rounded-[4px] bg-[var(--bg-tertiary)] border border-[var(--border-light)] font-mono text-xs text-[var(--text-tertiary)]">
                    {selectedEmail.folder}
                  </span>
                )}
              </div>

              <div className="flex items-center gap-2 flex-shrink-0">
                {resolvedTheme === 'dark' && selectedDetail?.body_html && (
                  <Tooltip content={paperLight ? '切换为深色阅读背景' : '切换为白底信笺模式'}>
                    <button
                      type="button"
                      onClick={() => setPaperLight(prev => !prev)}
                      className="btn btn-secondary btn-sm px-2"
                      aria-label={paperLight ? '切换为深色阅读背景' : '切换为白底信笺模式'}
                    >
                      {paperLight ? (
                        <Moon className="w-4 h-4" strokeWidth={1.5} />
                      ) : (
                        <Sun className="w-4 h-4" strokeWidth={1.5} />
                      )}
                      <span className="hidden xl:inline">{paperLight ? '深色正文' : '白底信笺'}</span>
                    </button>
                  </Tooltip>
                )}
                <button
                  type="button"
                  onClick={() => handleSendToTelegram(selectedEmail)}
                  disabled={sendingTelegram}
                  className="btn btn-secondary btn-sm"
                  title="转发到 Telegram (t)"
                  aria-label="转发到 Telegram"
                >
                  {sendingTelegram ? (
                    <Loader2 className="w-4 h-4 animate-spin" strokeWidth={1.5} />
                  ) : (
                    <Send className="w-4 h-4" strokeWidth={1.5} />
                  )}
                  <span className="hidden xl:inline">转发 Telegram</span>
                </button>
              </div>
            </div>

            {/* Mail Body Canvas */}
            <div ref={detailScrollRef} className="reading-canvas flex-1 overflow-auto thin-scrollbar">
              <article className="mail-letter max-w-3xl mx-auto rounded-xl border border-[var(--border-light)] bg-[var(--bg-primary)] shadow-[var(--shadow-sm)] overflow-hidden">
                <header className="mail-letter-header border-b border-[var(--border-light)]">
                  <h1 className="text-xl xl:text-2xl font-medium text-[var(--text-primary)] break-words leading-snug">
                    {selectedEmail.subject || '(无主题)'}
                  </h1>
                  {selectedEmailAccount && (
                    <div className="mt-3 flex items-start gap-2 text-xs text-[var(--text-secondary)]">
                      <Mail aria-hidden="true" className="w-4 h-4 flex-shrink-0" strokeWidth={1.5} />
                      <span className="flex-shrink-0">收件账户</span>
                      <span className="break-all">{selectedEmailAccount.email}</span>
                    </div>
                  )}

                  <div className="mt-4 flex flex-col gap-3 2xl:flex-row 2xl:items-center 2xl:justify-between">
                    <div className="flex items-start gap-3 min-w-0">
                      <div className="avatar">
                        {(selectedEmail.from_name || selectedEmail.from)?.[0]?.toUpperCase() || '?'}
                      </div>
                      <div className="min-w-0">
                        <div className="flex flex-col items-start gap-1">
                          <span className="text-sm font-medium text-[var(--text-primary)]">
                            {selectedEmail.from_name || selectedEmail.from}
                          </span>
                          <button
                            type="button"
                            onClick={() => {
                              navigator.clipboard?.writeText(selectedEmail.from).then(() => {
                                showToast('已复制发件人地址', 'success')
                              }).catch(() => {})
                            }}
                            className="sender-address inline-flex max-w-full items-center gap-2 px-1 py-1 -mx-1 rounded-[4px] font-mono text-xs text-[var(--text-tertiary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-tertiary)] transition-colors cursor-pointer"
                            title="点击复制发件人邮箱"
                          >
                            <span className="truncate">{selectedEmail.from}</span>
                            <Copy className="w-4 h-4 flex-shrink-0" strokeWidth={1.5} />
                          </button>
                        </div>
                        {selectedEmail.to && (
                          <div className="text-xs text-[var(--text-tertiary)] break-all mt-1">
                            收件人：<span className="font-mono">{selectedEmail.to}</span>
                          </div>
                        )}
                      </div>
                    </div>

                    <div className="flex items-center gap-2 flex-shrink-0">
                      {selectedEmail.has_attachment && (
                        <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-[4px] bg-[var(--bg-tertiary)] border border-[var(--border-light)] text-xs text-[var(--text-secondary)]">
                          <Paperclip className="w-4 h-4" strokeWidth={1.5} />
                          <span>附件</span>
                        </span>
                      )}
                      <time className="font-mono tabular-nums text-xs text-[var(--text-tertiary)]">
                        {new Date(selectedEmail.date).toLocaleString('zh-CN')}
                      </time>
                    </div>
                  </div>
                </header>

                <div
                  className={
                    useLightPaper && resolvedTheme === 'dark'
                      ? 'email-paper rounded-[8px] border border-[var(--border-default)] bg-[#FAFAFA]'
                      : 'mail-letter-body'
                  }
                >
                  {loadingDetail ? (
                    <div className="flex items-center justify-center py-12 gap-2 text-sm text-[var(--text-tertiary)]">
                      <Loader2 className="w-4 h-4 animate-spin" strokeWidth={1.5} />
                      <span>正在加载正文</span>
                    </div>
                  ) : detailError ? (
                    <div role="alert" className="text-sm text-[var(--error-700)]">
                      <p>{detailError}</p>
                      <button type="button" className="btn btn-secondary btn-sm mt-3" onClick={() => void retryDetail()}>重试加载正文</button>
                    </div>
                  ) : selectedDetail?.body_html ? (
                    <EmailBody key={selectedDetail.id} html={selectedDetail.body_html} light={useLightPaper} />
                  ) : selectedDetail?.body_text ? (
                    <pre className="whitespace-pre-wrap break-words text-sm leading-relaxed text-[var(--text-primary)] font-sans">
                      {selectedDetail.body_text}
                    </pre>
                  ) : (
                    <p className="text-sm text-[var(--text-tertiary)]">无邮件内容</p>
                  )}
                </div>
              </article>
            </div>
          </div>
        ) : (
          <div className="h-full flex items-center justify-center p-8 text-[var(--text-tertiary)]">
            <div className="text-center max-w-xs">
              <Mail className="w-10 h-10 mx-auto mb-4 opacity-35" strokeWidth={1.5} />
              <p className="text-sm font-medium text-[var(--text-secondary)]">选择一封邮件阅读</p>
              <p className="mt-2 text-xs text-[var(--text-tertiary)] leading-relaxed">
                使用 <kbd className="kbd">j</kbd> <kbd className="kbd">k</kbd> 移动，或按 <kbd className="kbd">?</kbd> 查看快捷键
              </p>
            </div>
          </div>
        )}
      </section>
  )
}
