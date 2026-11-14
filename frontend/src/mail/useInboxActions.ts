import { useCallback, useEffect, useRef, useState } from 'react'
import axios from 'axios'
import { telegramApi, type EmailSummary } from '../api'
import type { MailStore } from './MailStore'

export function useInboxActions(accounts: { length: number }, selectedAccountId: number | null, store: MailStore, loadAccounts: () => Promise<boolean>) {
  const [refreshing, setRefreshing] = useState(false)
  const [sendingTelegram, setSendingTelegram] = useState(false)
  const mounted = useRef(false)
  const lifecycle = useRef(0)
  const refreshPending = useRef(false)
  const showToast = store.notify
  useEffect(() => {
    mounted.current = true
    const generation = lifecycle.current + 1
    lifecycle.current = generation
    return () => { mounted.current = false; lifecycle.current = generation + 1 }
  }, [])
  const handleRefresh = useCallback(async () => {
    if (refreshPending.current || accounts.length === 0) return
    const generation = lifecycle.current
    refreshPending.current = true
    setRefreshing(true)
    try {
      const res = await store.refresh(selectedAccountId || undefined)
      const accountsUpdated = await loadAccounts()
      if (!mounted.current || lifecycle.current !== generation) return
      const errorEntries = res.errors ? Object.entries(res.errors) : []
      const messages: string[] = []
      if (errorEntries.length > 0) {
        const [failedAccount, failedReason] = errorEntries[0]
        messages.push(errorEntries.length === 1
          ? `${failedAccount} 刷新失败: ${failedReason}`
          : `刷新完成（${errorEntries.length} 个账户失败，如 ${failedAccount}）`)
      }
      if (!res.listUpdated) messages.push('邮箱同步请求已完成，但邮件列表未能更新，请重试读取邮件列表')
      if (!accountsUpdated) messages.push('账户列表未能更新，请重试读取账户')
      if (messages.length > 0) showToast(messages.join('；'), 'error')
      else showToast(res.new_count > 0 ? `已刷新，新增 ${res.new_count} 封邮件` : '已是最新邮件', 'success')
    } catch (e) {
      if (!mounted.current || lifecycle.current !== generation) return
      const message = axios.isAxiosError(e) ? e.response?.data?.error : undefined
      showToast(message || '刷新失败', 'error')
    } finally {
      if (mounted.current && lifecycle.current === generation) {
        refreshPending.current = false
        setRefreshing(false)
      }
    }
  }, [accounts.length, selectedAccountId, showToast, store, loadAccounts])

  const handleSendToTelegram = useCallback(async (email: EmailSummary) => {
    setSendingTelegram(true)
    try {
      await telegramApi.sendEmail(email.id)
      if (!mounted.current) return
      showToast('已转发到 Telegram', 'success')
    } catch (e) {
      const message = axios.isAxiosError(e) ? e.response?.data?.error : undefined
      if (!mounted.current) return
      showToast(message || '转发失败', 'error')
    } finally {
      if (mounted.current) setSendingTelegram(false)
    }
  }, [showToast])

  return { refreshing, sendingTelegram, handleRefresh, handleSendToTelegram }
}
