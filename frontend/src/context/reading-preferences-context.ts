import { createContext } from 'react'
import type { EmailLimit } from '../mail/limits'

export interface ReadingPreferencesContextType {
  blockRemoteImages: boolean
  setBlockRemoteImages: (blocked: boolean) => void
  emailLimit: EmailLimit
  setEmailLimit: (limit: EmailLimit) => void
}

const ReadingPreferencesContext = createContext<ReadingPreferencesContextType | undefined>(undefined)

export default ReadingPreferencesContext
