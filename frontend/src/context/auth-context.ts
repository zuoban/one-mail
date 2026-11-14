import { createContext } from 'react'

export interface AuthContextType {
  passwordRequired: boolean | null
  authenticated: boolean
  token: string | null
  loading: boolean
  error: string | null
  sessionGeneration: number
  isSessionCurrent: (generation: number) => boolean
  retry: () => Promise<void>
  login: (password: string) => Promise<void>
  logout: () => void
}

const AuthContext = createContext<AuthContextType | undefined>(undefined)

export default AuthContext
