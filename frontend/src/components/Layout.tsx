import { Outlet, Link, useLocation } from 'react-router-dom'
import { Users, Inbox, LogOut, Settings, Sun, Moon } from 'lucide-react'
import useAuth from '../context/useAuth'
import useTheme from '../context/useTheme'
import Tooltip from './Tooltip'

export default function Layout() {
  const location = useLocation()
  const { user, logout } = useAuth()
  const { resolvedTheme, setTheme } = useTheme()

  const navItems = [
    { path: '/inbox', label: '收件箱', icon: Inbox },
    { path: '/accounts', label: '邮箱账户', icon: Users },
    { path: '/settings', label: '设置', icon: Settings },
  ]
  const currentNav = navItems.find((item) => location.pathname.startsWith(item.path)) ?? navItems[0]

  return (
    <div className="min-h-screen md:h-screen flex flex-col md:flex-row bg-[var(--bg-secondary)]">
      {/* Mobile Header */}
      <header className="safe-area-top md:hidden sticky top-0 z-30 border-b border-[var(--border-light)] bg-[var(--bg-sidebar)]">
        <div className="flex h-14 items-center justify-between px-4">
          <div className="flex items-center gap-2.5 min-w-0">
            <img
              src={resolvedTheme === 'dark' ? '/logo-dark.svg' : '/logo.svg'}
              alt="One-Mail"
              className="w-7 h-7 rounded-[6px] flex-shrink-0"
            />
            <div className="min-w-0 flex items-baseline gap-2">
              <span className="text-[15px] font-medium tracking-[-0.01em] text-[var(--text-primary)]">
                One-Mail
              </span>
              <span className="text-xs text-[var(--text-tertiary)] truncate">
                {currentNav.label}
              </span>
            </div>
          </div>
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={() => setTheme(resolvedTheme === 'dark' ? 'light' : 'dark')}
              className="btn btn-ghost w-9 h-9 p-0"
              aria-label={resolvedTheme === 'dark' ? '切换到浅色模式' : '切换到深色模式'}
              title={resolvedTheme === 'dark' ? '切换到浅色模式' : '切换到深色模式'}
            >
              {resolvedTheme === 'dark' ? (
                <Sun className="w-4 h-4" strokeWidth={1.5} />
              ) : (
                <Moon className="w-4 h-4" strokeWidth={1.5} />
              )}
            </button>
            <button
              type="button"
              onClick={logout}
              className="btn btn-ghost w-9 h-9 p-0"
              aria-label="退出登录"
              title="退出登录"
            >
              <LogOut className="w-4 h-4" strokeWidth={1.5} />
            </button>
          </div>
        </div>
      </header>

      {/* Desktop Sidebar */}
      <aside className="hidden md:flex w-60 flex-shrink-0 bg-[var(--bg-sidebar)] border-r border-[var(--border-light)] flex-col">
        <div className="h-14 px-4 border-b border-[var(--border-light)] flex items-center">
          <Link to="/inbox" className="flex items-center gap-2.5 text-[var(--text-primary)]">
            <img
              src={resolvedTheme === 'dark' ? '/logo-dark.svg' : '/logo.svg'}
              alt="One-Mail"
              className="w-7 h-7 rounded-[6px]"
            />
            <span className="text-[16px] font-medium tracking-[-0.01em]">One-Mail</span>
          </Link>
        </div>

        <nav className="flex-1 px-3 py-4" aria-label="主导航">
          <div className="px-3 mb-2 text-xs text-[var(--text-tertiary)]">工作区</div>
          <div className="space-y-0.5">
            {navItems.map((item) => {
              const Icon = item.icon
              const isActive = location.pathname.startsWith(item.path)
              return (
                <Link
                  key={item.path}
                  to={item.path}
                  style={isActive ? { boxShadow: 'var(--active-highlight)' } : undefined}
                  className={`flex h-9 items-center gap-2.5 px-3 rounded-[8px] text-sm border transition-colors ${
                    isActive
                      ? 'bg-[var(--bg-nav-active)] text-[var(--text-primary)] font-medium border-[var(--border-light)]'
                      : 'border-transparent text-[var(--text-secondary)] hover:bg-[var(--bg-nav-hover)] hover:text-[var(--text-primary)]'
                  }`}
                >
                  <span className="flex w-4 h-4 items-center justify-center flex-shrink-0">
                    <Icon
                      className={`w-4 h-4 ${isActive ? 'text-[var(--primary-500)]' : 'text-[var(--text-tertiary)]'}`}
                      strokeWidth={1.5}
                    />
                  </span>
                  <span className="truncate">{item.label}</span>
                </Link>
              )
            })}
          </div>
        </nav>

        <div className="h-12 px-3 border-t border-[var(--border-light)] flex items-center flex-shrink-0">
          <div className="flex w-full items-center justify-between gap-2 px-1">
            <div className="flex items-center gap-2.5 min-w-0">
              <div className="avatar avatar-sm">
                {user?.username?.[0]?.toUpperCase() || '?'}
              </div>
              <span className="text-sm font-medium text-[var(--text-primary)] truncate">
                {user?.username}
              </span>
            </div>
            <div className="flex items-center gap-0.5 flex-shrink-0">
              <Tooltip content={resolvedTheme === 'dark' ? '浅色模式' : '深色模式'}>
                <button
                  type="button"
                  onClick={() => setTheme(resolvedTheme === 'dark' ? 'light' : 'dark')}
                  className="btn btn-ghost w-8 h-8 p-0"
                  aria-label={resolvedTheme === 'dark' ? '切换到浅色模式' : '切换到深色模式'}
                >
                  {resolvedTheme === 'dark' ? (
                    <Sun className="w-4 h-4 text-[var(--text-tertiary)]" strokeWidth={1.5} />
                  ) : (
                    <Moon className="w-4 h-4 text-[var(--text-tertiary)]" strokeWidth={1.5} />
                  )}
                </button>
              </Tooltip>
              <Tooltip content="退出登录">
                <button
                  type="button"
                  onClick={logout}
                  className="btn btn-ghost w-8 h-8 p-0"
                  aria-label="退出登录"
                >
                  <LogOut className="w-4 h-4 text-[var(--text-tertiary)]" strokeWidth={1.5} />
                </button>
              </Tooltip>
            </div>
          </div>
        </div>
      </aside>

      {/* Main Content */}
      <main className="safe-area-bottom-margin flex-1 min-h-0 overflow-auto pb-20 md:pb-0">
        <Outlet />
      </main>

      {/* Mobile Bottom Nav */}
      <nav className="safe-area-bottom md:hidden fixed inset-x-0 bottom-0 z-30 border-t border-[var(--border-light)] bg-[var(--bg-sidebar)]">
        <div className="grid grid-cols-4 gap-1 px-2 py-1.5">
          {navItems.map((item) => {
            const Icon = item.icon
            const isActive = location.pathname.startsWith(item.path)
            return (
              <Link
                key={item.path}
                to={item.path}
                style={isActive ? { boxShadow: 'var(--active-highlight)' } : undefined}
                className={`flex min-h-12 flex-col items-center justify-center gap-1 rounded-[8px] px-2 py-1.5 text-xs border transition-colors ${
                  isActive
                    ? 'bg-[var(--bg-nav-active)] text-[var(--text-primary)] font-medium border-[var(--border-light)]'
                    : 'border-transparent text-[var(--text-secondary)]'
                }`}
              >
                <Icon
                  className={`w-4 h-4 ${isActive ? 'text-[var(--primary-500)]' : 'text-[var(--text-tertiary)]'}`}
                  strokeWidth={1.5}
                />
                <span>{item.label}</span>
              </Link>
            )
          })}
        </div>
      </nav>
    </div>
  )
}
