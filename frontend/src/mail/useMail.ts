import { useContext, useEffect, useSyncExternalStore } from 'react'
import { MailContext } from './mail-context'
import type { MailQuery } from './MailStore'

export default function useMail(query: MailQuery) {
  const store = useContext(MailContext)
  if (!store) throw new Error('useMail must be used within MailProvider')
  const state = useSyncExternalStore(store.subscribe, store.getSnapshot)
  const { search, account_id } = query
  useEffect(() => {
    store.setQuery({ search, account_id })
    return store.closeDetail
  }, [store, search, account_id])
  return { ...state, store }
}
