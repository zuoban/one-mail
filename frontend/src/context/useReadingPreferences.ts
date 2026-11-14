import { useContext } from 'react'
import ReadingPreferencesContext from './reading-preferences-context'

export default function useReadingPreferences() {
  const context = useContext(ReadingPreferencesContext)
  if (context === undefined) {
    throw new Error('useReadingPreferences must be used within ReadingPreferencesProvider')
  }
  return context
}
