import { accountApi, providerApi, type AccountInput, type EmailAccount, type EmailProvider } from '../api'

interface AccountState {
  accounts: EmailAccount[]
  providers: EmailProvider[]
  loadingAccounts: boolean
  loadingProviders: boolean
  accountsError: string
  providersError: string
}

type Resource = 'accounts' | 'providers'

export class AccountStore {
  private state: AccountState = { accounts: [], providers: [], loadingAccounts: true, loadingProviders: true, accountsError: '', providersError: '' }
  private listeners = new Set<() => void>()
  private requests: Partial<Record<Resource, AbortController>> = {}
  private loaded = new Set<Resource>()
  private disposed = false

  private sessionValid: () => boolean
  private invalidateMail: (accountId: number) => void

  constructor(sessionValid: () => boolean = () => true, invalidateMail: (accountId: number) => void = () => {}) {
    this.sessionValid = sessionValid
    this.invalidateMail = invalidateMail
  }

  private current = () => !this.disposed && this.sessionValid()
  getSnapshot = () => this.state
  subscribe = (listener: () => void) => {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
      if (this.listeners.size === 0) {
        for (const resource of ['accounts', 'providers'] as const) {
          const controller = this.requests[resource]
          if (controller) {
            controller.abort()
            this.loaded.delete(resource)
          }
        }
        this.requests = {}
      }
    }
  }

  private publish(patch: Partial<AccountState>) {
    if (!this.current()) return
    this.state = { ...this.state, ...patch }
    this.listeners.forEach(listener => listener())
  }

  ensure = () => {
    for (const resource of ['accounts', 'providers'] as const) {
      if (!this.loaded.has(resource) && !this.requests[resource]) void this.load(resource)
    }
  }

  private async load(resource: Resource): Promise<boolean> {
    if (!this.current()) return false
    this.requests[resource]?.abort()
    const controller = new AbortController()
    this.requests[resource] = controller
    this.publish(resource === 'accounts' ? { loadingAccounts: true, accountsError: '' } : { loadingProviders: true, providersError: '' })
    const valid = () => this.current() && !controller.signal.aborted && this.requests[resource] === controller
    try {
      if (resource === 'accounts') {
        const response = await accountApi.list(controller.signal)
        if (!valid()) return false
        this.publish({ accounts: response.data })
      } else {
        const response = await providerApi.list(controller.signal)
        if (!valid()) return false
        this.publish({ providers: response.data })
      }
      this.loaded.add(resource)
      return true
    } catch {
      if (valid()) {
        this.loaded.add(resource)
        this.publish(resource === 'accounts' ? { accountsError: '账户列表加载失败，请重试' } : { providersError: '邮箱服务商加载失败，请重试' })
      }
      return false
    } finally {
      if (valid()) {
        delete this.requests[resource]
        this.publish(resource === 'accounts' ? { loadingAccounts: false } : { loadingProviders: false })
      }
    }
  }

  reload = () => this.load('accounts')
  retryProviders = () => this.load('providers')

  invalidate = () => {
    if (!this.current()) return Promise.resolve(false)
    this.loaded.delete('accounts')
    return this.listeners.size > 0 ? this.reload() : Promise.resolve(false)
  }

  private async mutate<T>(operation: () => Promise<T>) {
    const response = await operation()
    // The write has succeeded independently of the subsequent list read.
    const updated = this.invalidate()
    return { response, updated }
  }

  add = (input: AccountInput) => this.mutate(() => accountApi.add(input))
  update = (id: number, input: Partial<AccountInput>) => {
    const account = this.state.accounts.find(account => account.id === id)
    const identityChanged = !account || (
      (input.email !== undefined && input.email !== account.email) ||
      (input.provider !== undefined && input.provider !== account.provider) ||
      (input.username !== undefined && input.username !== account.username) ||
      (input.imap_host !== undefined && input.imap_host !== account.imap_host) ||
      (input.imap_port !== undefined && input.imap_port !== account.imap_port)
    )
    return this.mutate(async () => {
      const response = await accountApi.update(id, input)
      if (this.current() && identityChanged) this.invalidateMail(id)
      return response
    })
  }
  remove = (id: number) => this.mutate(async () => {
    const response = await accountApi.delete(id)
    if (this.current()) this.invalidateMail(id)
    return response
  })
  refresh = (id: number) => this.mutate(() => accountApi.refresh(id))

  dispose = () => {
    this.disposed = true
    for (const controller of Object.values(this.requests)) controller?.abort()
    this.requests = {}
    this.listeners.clear()
    this.state = { accounts: [], providers: [], loadingAccounts: false, loadingProviders: false, accountsError: '', providersError: '' }
  }
}
