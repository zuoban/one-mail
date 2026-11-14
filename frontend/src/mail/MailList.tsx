import { Loader2, Mail } from 'lucide-react'
import type { RefObject } from 'react'
import type { EmailAccount, EmailSummary } from '../api'
import { EmailListRow } from './EmailListRow'

interface Props {
  emails: EmailSummary[]
  selectedId?: number
  accountById: Map<number, EmailAccount>
  showAccountEmail: boolean
  limit: number
  dateLabels: Map<number, { label: string }>
  loading: boolean
  refreshing: boolean
  error: string | null
  search: string
  scrollRef: RefObject<HTMLDivElement | null>
  retry: () => Promise<unknown>
  clearSearch: () => void
  registerRef: (id: number, node: HTMLDivElement | null) => void
  onSelect: (email: EmailSummary) => Promise<void>
}

export default function MailList({ emails, selectedId, accountById, showAccountEmail, limit, dateLabels, loading, refreshing, error, search, scrollRef, retry, clearSearch, registerRef, onSelect }: Props) {
  return (
        <div ref={scrollRef} className="flex-1 overflow-auto thin-scrollbar">
          {error && <div role="alert" className="p-4 text-sm text-[var(--error-700)]">{error}<button type="button" disabled={loading} onClick={() => void retry()} className="btn btn-secondary btn-sm ml-2">重试</button></div>}
          {(loading || refreshing) && emails.length === 0 ? (
            <div className="empty-state h-full min-h-48 px-6">
              <div className="refresh-empty-icon" aria-hidden="true">
                <Loader2 className="w-6 h-6 animate-spin" strokeWidth={1.5} />
              </div>
              <p className="text-sm font-medium text-[var(--text-primary)]">{refreshing ? '正在刷新邮件' : '正在加载邮件'}</p>
              <p className="mt-2 text-xs leading-5 text-[var(--text-secondary)]">{refreshing ? `连接邮箱并获取最近 ${limit} 封邮件` : '正在读取邮件列表'}</p>
            </div>
          ) : emails.length === 0 && !error ? (
            <div className="empty-state">
              <Mail className="empty-state-icon" />
              <p className="text-sm font-medium text-[var(--text-primary)] mb-1">暂无邮件</p>
              <p className="text-xs text-[var(--text-tertiary)] mb-4">
                {search
                  ? '没有匹配当前关键词的邮件'
                  : `添加邮箱账户并点击刷新获取最近 ${limit} 封邮件`}
              </p>
              {search && (
                <button
                  type="button"
                  onClick={() => {
                    clearSearch()
                  }}
                  className="btn btn-secondary btn-sm"
                >
                  显示全部邮件
                </button>
              )}
            </div>
          ) : (
            <div className="p-2">
              {emails.map(email => (
                <EmailListRow
                  key={email.id}
                  email={email}
                  accountEmail={showAccountEmail ? accountById.get(email.account_id)?.email : undefined}
                  dateLabel={dateLabels.get(email.id)?.label ?? '未知'}
                  isSelected={selectedId === email.id}
                  registerRef={registerRef}
                  onSelect={onSelect}
                />
              ))}

              {emails.length > 0 && (
                <div className="text-center py-4 font-mono tabular-nums text-xs text-[var(--text-tertiary)]">
                  仅展示最近 {limit} 封邮件 · 当前 {emails.length} 封
                </div>
              )}
            </div>
          )}
        </div>

  )
}
