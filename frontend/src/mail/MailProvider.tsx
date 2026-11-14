import { useEffect, useRef, useState, type ReactNode } from 'react'
import useAuth from '../context/useAuth'
import { MailStore } from './MailStore'
import { MailContext } from './mail-context'

function SessionMailProvider({ token, children }: { token: string; children: ReactNode }) {
  const [store] = useState(() => new MailStore(undefined, () => localStorage.getItem('token') === token))
  const cleanupTimer = useRef<number | undefined>(undefined)
  useEffect(() => {
    window.clearTimeout(cleanupTimer.current)
    // Defer disposal one turn so React's development effect replay can retain the store.
    // The captured session check already prevents work immediately after logout.
    return () => { cleanupTimer.current = window.setTimeout(store.dispose, 0) }
  }, [store])
  return <MailContext.Provider value={store}>{children}</MailContext.Provider>
}

export default function MailProvider({ children }: { children: ReactNode }) {
  const { token, user } = useAuth()
  if (!token || !user) return null
  return <SessionMailProvider key={`${user.id}:${token}`} token={token}>{children}</SessionMailProvider>
}
