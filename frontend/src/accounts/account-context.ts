import { createContext, useContext, useEffect, useSyncExternalStore } from 'react'
import { AccountStore } from './AccountStore'

export const AccountContext = createContext<AccountStore | null>(null)

export function useAccounts() {
  const store = useContext(AccountContext)
  if (!store) throw new Error('useAccounts requires MailProvider')
  const snapshot = useSyncExternalStore(store.subscribe, store.getSnapshot)
  useEffect(() => { store.ensure() }, [store])
  return { ...snapshot, accountStore: store, loadAccounts: store.reload }
}
