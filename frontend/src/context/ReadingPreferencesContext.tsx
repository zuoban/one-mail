import { useEffect, useState, type ReactNode } from 'react'
import ReadingPreferencesContext from './reading-preferences-context'
import { DEFAULT_EMAIL_LIMIT, EMAIL_LIMIT_OPTIONS, type EmailLimit } from '../mail/limits'

export function ReadingPreferencesProvider({ children }: { children: ReactNode }) {
  const [blockRemoteImages, setBlockRemoteImages] = useState(() => localStorage.getItem('blockRemoteImages') === 'true')
  const [emailLimit, setEmailLimit] = useState<EmailLimit>(() => {
    const stored = Number(localStorage.getItem('emailLimit'))
    return EMAIL_LIMIT_OPTIONS.find(limit => limit === stored) ?? DEFAULT_EMAIL_LIMIT
  })

  useEffect(() => {
    localStorage.setItem('blockRemoteImages', String(blockRemoteImages))
  }, [blockRemoteImages])

  useEffect(() => {
    localStorage.setItem('emailLimit', String(emailLimit))
  }, [emailLimit])

  return (
    <ReadingPreferencesContext.Provider value={{ blockRemoteImages, setBlockRemoteImages, emailLimit, setEmailLimit }}>
      {children}
    </ReadingPreferencesContext.Provider>
  )
}
