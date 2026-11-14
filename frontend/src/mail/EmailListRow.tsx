import { memo } from 'react'
import { Paperclip } from 'lucide-react'
import type { EmailSummary } from '../api'

interface EmailListRowProps {
  email: EmailSummary
  accountEmail?: string
  dateLabel: string
  isSelected: boolean
  registerRef: (id: number, node: HTMLDivElement | null) => void
  onSelect: (email: EmailSummary) => void
}

export const EmailListRow = memo(function EmailListRow({
  email,
  accountEmail,
  dateLabel,
  isSelected,
  registerRef,
  onSelect,
}: EmailListRowProps) {
  return (
    <div
      ref={(node) => registerRef(email.id, node)}
      role="group"
      tabIndex={0}
      aria-label={`${email.from_name || email.from}，${email.subject || '无主题'}`}
      aria-describedby={accountEmail ? `mail-account-${email.id}` : undefined}
      aria-current={isSelected ? 'true' : undefined}
      onKeyDown={event => {
        if (event.target !== event.currentTarget) return
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          onSelect(email)
        }
      }}
      onClick={() => onSelect(email)}
      style={{ minHeight: accountEmail ? 96 : 72, marginBottom: 4 }}
      className={`mail-row group px-3 py-3 rounded-[8px] border cursor-pointer transition-colors ${
        isSelected
          ? 'bg-[var(--bg-accent-soft)] border-[var(--primary-200)]'
          : 'border-transparent hover:bg-[var(--bg-nav-hover)]'
      }`}
    >
      <div className="flex items-start gap-2">
        <div className="flex-1 min-w-0">
          <div className="flex h-5 items-center justify-between gap-2">
            <div className="flex items-center gap-2 min-w-0">
              <span
                className="text-[13px] truncate text-[var(--text-secondary)]"
              >
                {email.from_name || email.from}
              </span>
            </div>

            <div className="relative flex items-center justify-end flex-shrink-0 min-w-12">
              <span className="font-mono tabular-nums text-xs text-[var(--text-tertiary)] whitespace-nowrap">
                {dateLabel}
              </span>
            </div>
          </div>

          <div className="mt-1 flex items-center gap-2">
            <p
              className="text-sm truncate flex-1 text-[var(--text-primary)]"
            >
              {email.subject || '(无主题)'}
            </p>
            {email.has_attachment && (
              <Paperclip className="w-4 h-4 text-[var(--text-tertiary)] flex-shrink-0" strokeWidth={1.5} />
            )}
          </div>
          {accountEmail && (
            <div id={`mail-account-${email.id}`} className="mt-1 flex items-center gap-1.5 min-w-0 text-xs text-[var(--text-secondary)]">
              <span className="flex-shrink-0">收至</span>
              <span className="truncate" title={accountEmail}>{accountEmail}</span>
            </div>
          )}
        </div>
      </div>
    </div>
  )
})

