import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import Dashboard from './pages/Dashboard'
import Accounts from './pages/Accounts'
import Layout from './components/Layout'

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Layout />}>
          <Route index element={<Navigate to="/inbox" replace />} />
          <Route path="inbox" element={<Dashboard />} />
          <Route path="accounts" element={<Accounts />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}

export default App