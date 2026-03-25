import { Outlet, Link, useLocation } from 'react-router-dom'
import { Users, Inbox, LogOut, Settings, Sun, Moon } from 'lucide-react'
import useAuth from '../context/useAuth'
import { useTheme } from '../context/ThemeContext'

export default function Layout() {
  const location = useLocation()
  const { user, logout } = useAuth()
  const { theme, resolvedTheme, setTheme } = useTheme()

  const navItems = [
    { path: '/inbox', label: '收件箱', icon: Inbox },
    { path: '/accounts', label: '邮箱账户', icon: Users },
    { path: '/settings', label: '设置', icon: Settings },
  ]
  const currentNav = navItems.find((item) => location.pathname.startsWith(item.path)) ?? navItems[0]

  return (
    <div className="min-h-screen md:h-screen flex flex-col md:flex-row bg-[var(--bg-secondary)]">
      <header className="safe-area-top md:hidden sticky top-0 z-30 border-b border-[var(--border-light)] bg-[var(--bg-primary)]/95 backdrop-blur">
        <div className="flex items-center justify-between px-4 py-3">
          <div className="min-w-0">
            <div className="flex items-center gap-3">
              <img src={resolvedTheme === 'dark' ? '/logo-dark.svg' : '/logo.svg'} alt="One-Mail" className="w-8 h-8 rounded-lg flex-shrink-0" />
              <div className="min-w-0">
                <div className="text-sm font-semibold text-[var(--text-primary)]">One-Mail</div>
                <div className="text-xs text-[var(--text-tertiary)] truncate">{currentNav.label}</div>
              </div>
            </div>
          </div>
          <div className="flex items-center gap-1">
            <button
              onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
              className="btn btn-ghost p-2 min-h-11 min-w-11"
              title={theme === 'dark' ? '切换到浅色模式' : '切换到深色模式'}
            >
              {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
            </button>
            <button
              onClick={logout}
              className="btn btn-ghost p-2 min-h-11 min-w-11"
              title="退出登录"
            >
              <LogOut className="w-4 h-4" />
            </button>
          </div>
        </div>
      </header>

      <aside className="hidden md:flex w-64 bg-[var(--bg-primary)] border-r border-[var(--border-light)] flex-col">
        <div className="p-5 border-b border-[var(--border-light)]">
          <h1 className="text-xl font-semibold flex items-center gap-3 text-[var(--text-primary)]">
            <img src={resolvedTheme === 'dark' ? '/logo-dark.svg' : '/logo.svg'} alt="One-Mail" className="w-9 h-9 rounded-lg" />
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
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-3">
              <div className="avatar avatar-sm">
                {user?.username?.[0]?.toUpperCase() || '?'}
              </div>
              <span className="text-sm font-medium text-[var(--text-primary)]">{user?.username}</span>
            </div>
            <div className="flex items-center gap-1">
              <button
                onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
                className="btn btn-ghost p-2"
                title={theme === 'dark' ? '切换到浅色模式' : '切换到深色模式'}
              >
                {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
              </button>
              <button
                onClick={logout}
                className="btn btn-ghost p-2"
                title="退出登录"
              >
                <LogOut className="w-4 h-4" />
              </button>
            </div>
          </div>
        </div>
      </aside>
      <main className="safe-area-bottom-margin flex-1 min-h-0 overflow-auto pb-20 md:pb-0">
        <Outlet />
      </main>

      <nav className="safe-area-bottom md:hidden fixed inset-x-0 bottom-0 z-30 border-t border-[var(--border-light)] bg-[var(--bg-primary)]/95 backdrop-blur">
        <div className="grid grid-cols-3 gap-1 px-2 py-2">
          {navItems.map((item) => {
            const Icon = item.icon
            const isActive = location.pathname === item.path
            return (
              <Link
                key={item.path}
                to={item.path}
                className={`flex min-h-14 flex-col items-center justify-center gap-1 rounded-xl px-2 py-2 text-xs font-medium transition-colors ${
                  isActive
                    ? 'bg-[var(--primary-50)] text-[var(--primary-700)]'
                    : 'text-[var(--text-secondary)]'
                }`}
              >
                <Icon className="w-4 h-4" />
                <span>{item.label}</span>
              </Link>
            )
          })}
        </div>
      </nav>
    </div>
  )
}
