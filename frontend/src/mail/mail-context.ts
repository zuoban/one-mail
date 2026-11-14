import { createContext } from 'react'
import type { MailStore } from './MailStore'

export const MailContext = createContext<MailStore | null>(null)
