import { useEffect, useRef, useState, type ReactNode } from 'react'
import useAuth from '../context/useAuth'
import { MailStore } from './MailStore'
import { MailContext } from './mail-context'
import { AccountStore } from '../accounts/AccountStore'
import { AccountContext } from '../accounts/account-context'

function SessionMailProvider({ generation, sessionValid, children }: { generation: number; sessionValid: (generation: number) => boolean; children: ReactNode }) {
  const [store] = useState(() => new MailStore(undefined, () => sessionValid(generation)))
  const [accounts] = useState(() => new AccountStore(() => sessionValid(generation), store.invalidateAccount))
  const cleanupTimer = useRef<number | undefined>(undefined)
  useEffect(() => {
    window.clearTimeout(cleanupTimer.current)
    // Defer disposal one turn so React's development effect replay can retain the store.
    // The captured session check already prevents work immediately after logout.
    return () => { cleanupTimer.current = window.setTimeout(() => { store.dispose(); accounts.dispose() }, 0) }
  }, [store, accounts])
  return <MailContext.Provider value={store}><AccountContext.Provider value={accounts}>{children}</AccountContext.Provider></MailContext.Provider>
}

export default function MailProvider({ children }: { children: ReactNode }) {
  const { authenticated, loading, error, sessionGeneration, isSessionCurrent } = useAuth()
  if (loading || error || !authenticated) return null
  return <SessionMailProvider key={sessionGeneration} generation={sessionGeneration} sessionValid={isSessionCurrent}>{children}</SessionMailProvider>
}
