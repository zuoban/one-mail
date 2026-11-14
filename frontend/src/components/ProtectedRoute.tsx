import { Navigate, useLocation } from 'react-router-dom'
import AuthStatusFeedback from './AuthStatusFeedback'
import useAuth from '../context/useAuth'

interface ProtectedRouteProps {
  children: React.ReactNode
}

export default function ProtectedRoute({ children }: ProtectedRouteProps) {
  const { authenticated, passwordRequired, loading, error } = useAuth()
  const location = useLocation()

  if (loading || error || passwordRequired === null) return <AuthStatusFeedback />

  if (!authenticated) {
    return <Navigate to="/login" state={{ from: location }} replace />
  }

  return <>{children}</>
}
