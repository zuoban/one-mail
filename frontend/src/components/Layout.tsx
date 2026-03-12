import { Outlet, Link, useLocation } from 'react-router-dom'
import { Mail, Users, Inbox, LogOut } from 'lucide-react'
import useAuth from '../context/useAuth'

export default function Layout() {
  const location = useLocation()
  const { user, logout } = useAuth()

  const navItems = [
    { path: '/inbox', label: '收件箱', icon: Inbox },
    { path: '/accounts', label: '邮箱账户', icon: Users },
  ]

  return (
    <div className="h-screen flex">
      <aside className="w-64 bg-white border-r border-gray-200 flex flex-col">
        <div className="p-4 border-b border-gray-200">
          <h1 className="text-xl font-bold flex items-center gap-2 text-gray-800">
            <Mail className="w-6 h-6 text-blue-600" />
            One-Mail
          </h1>
        </div>
        <nav className="flex-1 p-4">
          {navItems.map((item) => {
            const Icon = item.icon
            const isActive = location.pathname === item.path
            return (
              <Link
                key={item.path}
                to={item.path}
                className={`flex items-center gap-3 px-4 py-3 rounded-lg mb-2 transition-colors ${
                  isActive
                    ? 'bg-blue-50 text-blue-600'
                    : 'text-gray-600 hover:bg-gray-50'
                }`}
              >
                <Icon className="w-5 h-5" />
                <span className="font-medium">{item.label}</span>
              </Link>
            )
          })}
        </nav>
        <div className="p-4 border-t border-gray-200">
          <div className="flex items-center justify-between">
            <span className="text-sm text-gray-600">{user?.username}</span>
            <button
              onClick={logout}
              className="flex items-center gap-2 px-3 py-2 text-sm text-gray-600 hover:text-red-600 hover:bg-red-50 rounded-lg transition-colors"
            >
              <LogOut className="w-4 h-4" />
              退出
            </button>
          </div>
        </div>
      </aside>
      <main className="flex-1 min-h-0 overflow-hidden">
        <div className="h-full">
          <Outlet />
        </div>
      </main>
    </div>
  )
}
