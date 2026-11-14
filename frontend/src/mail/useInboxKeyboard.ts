import { useEffect, type Dispatch, type RefObject, type SetStateAction } from 'react'
import type { EmailSummary } from '../api'

interface Options {
  searchInputRef: RefObject<HTMLInputElement | null>
  sortedFilteredEmails: EmailSummary[]
  selectedEmail: EmailSummary | null
  showShortcuts: boolean
  setShowShortcuts: Dispatch<SetStateAction<boolean>>
  sendingTelegram: boolean
  handleRead: (email: EmailSummary, shouldScroll?: boolean, scrollDetail?: boolean) => Promise<void>
  handleSendToTelegram: (email: EmailSummary) => Promise<void>
  handleRefresh: () => Promise<void>
  closeDetail: () => void
}

export function useInboxKeyboard({ searchInputRef, sortedFilteredEmails, selectedEmail, showShortcuts, setShowShortcuts, sendingTelegram, handleRead, handleSendToTelegram, handleRefresh, closeDetail }: Options) {
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) {
        if (event.key === 'Escape' && target === searchInputRef.current) {
          searchInputRef.current?.blur()
        }
        return
      }

      if (event.key === '?') {
        event.preventDefault()
        setShowShortcuts(prev => !prev)
        return
      }

      if (showShortcuts) {
        if (event.key === 'Escape') setShowShortcuts(false)
        return
      }

      if (event.key === '/') {
        event.preventDefault()
        searchInputRef.current?.focus()
        return
      }

      if (event.key === 'j') {
        event.preventDefault()
        const index = sortedFilteredEmails.findIndex(email => email.id === selectedEmail?.id)
        const nextIdx = index + 1 < sortedFilteredEmails.length ? index + 1 : 0
        const nextEmail = sortedFilteredEmails[nextIdx]
        if (nextEmail) {
          handleRead(nextEmail, true)
        }
      } else if (event.key === 'k') {
        event.preventDefault()
        const index = sortedFilteredEmails.findIndex(email => email.id === selectedEmail?.id)
        const prevIdx = index <= 0 ? sortedFilteredEmails.length - 1 : index - 1
        const prevEmail = sortedFilteredEmails[prevIdx]
        if (prevEmail) {
          handleRead(prevEmail, true)
        }
      } else if (event.key === 'o' || event.key === 'O') {
        event.preventDefault()
        const targetEmail = selectedEmail || sortedFilteredEmails[0]
        if (targetEmail) handleRead(targetEmail, true, true)
      } else if (event.key === 't' || event.key === 'T') {
        if (selectedEmail && !sendingTelegram) {
          event.preventDefault()
          handleSendToTelegram(selectedEmail)
        }
      } else if (event.key === 'r' || event.key === 'R') {
        event.preventDefault()
        handleRefresh()
      } else if (event.key === 'Escape' && selectedEmail) {
        closeDetail()
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [
    handleRefresh,
    handleRead,
    handleSendToTelegram,
    sendingTelegram,
    selectedEmail,
    sortedFilteredEmails,
    showShortcuts,
    closeDetail,
    searchInputRef,
    setShowShortcuts,
  ])
}
