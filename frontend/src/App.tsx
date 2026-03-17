import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { AuthProvider } from './context/AuthContext'
import { ThemeProvider } from './context/ThemeContext'
import ProtectedRoute from './components/ProtectedRoute'
import Dashboard from './pages/Dashboard'
import Accounts from './pages/Accounts'
import SyncLogs from './pages/SyncLogs'
import Settings from './pages/Settings'
import Login from './pages/Login'
import Register from './pages/Register'
import Layout from './components/Layout'

function App() {
  return (
    <ThemeProvider>
      <AuthProvider>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route path="/register" element={<Register />} />
          
          <Route
            path="/"
            element={
              <ProtectedRoute>
                <Layout />
              </ProtectedRoute>
            }
          >
            <Route index element={<Navigate to="/inbox" replace />} />
            <Route path="inbox" element={<Dashboard />} />
            <Route path="accounts" element={<Accounts />} />
            <Route path="sync-logs" element={<SyncLogs />} />
            <Route path="settings" element={<Settings />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  </ThemeProvider>
  )
}

export default App