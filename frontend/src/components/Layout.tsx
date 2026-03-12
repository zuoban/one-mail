import { Outlet, Link, useLocation } from 'react-router-dom'
import { Users, Inbox, LogOut, FileText } from 'lucide-react'
import useAuth from '../context/useAuth'

export default function Layout() {
  const location = useLocation()
  const { user, logout } = useAuth()

  const navItems = [
    { path: '/inbox', label: '收件箱', icon: Inbox },
    { path: '/accounts', label: '邮箱账户', icon: Users },
    { path: '/sync-logs', label: '同步日志', icon: FileText },
  ]

  return (
    <div className="h-screen flex bg-[var(--bg-secondary)]">
      <aside className="w-64 bg-[var(--bg-primary)] border-r border-[var(--border-light)] flex flex-col">
        <div className="p-5 border-b border-[var(--border-light)]">
          <h1 className="text-xl font-semibold flex items-center gap-3 text-[var(--text-primary)]">
            <img src="/logo.svg" alt="One-Mail" className="w-9 h-9 rounded-lg" />
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
                className={`flex items-center gap-3 px-4 py-3 rounded-lg mb-1 transition-colors ${
                  isActive
                    ? 'bg-[var(--primary-50)] text-[var(--primary-700)]'
                    : 'text-[var(--text-secondary)] hover:bg-[var(--bg-tertiary)]'
                }`}
              >
                <Icon className="w-5 h-5" />
                <span className="font-medium">{item.label}</span>
              </Link>
            )
          })}
        </nav>
        <div className="p-4 border-t border-[var(--border-light)]">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <div className="avatar avatar-sm">
                {user?.username?.[0]?.toUpperCase() || '?'}
              </div>
              <span className="text-sm font-medium text-[var(--text-primary)]">{user?.username}</span>
            </div>
            <button
              onClick={logout}
              className="btn btn-ghost p-2"
              title="退出登录"
            >
              <LogOut className="w-4 h-4" />
            </button>
          </div>
        </div>
      </aside>
      <main className="flex-1 min-h-0 overflow-auto">
        <Outlet />
      </main>
    </div>
  )
}
