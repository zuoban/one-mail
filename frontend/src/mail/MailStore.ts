import { emailApi, type EmailDetail, type EmailSummary } from '../api'

export interface MailQuery { search?: string; account_id?: number }
export interface MailNotice { message: string; type: 'success' | 'error' }
export type MailReloadResult = 'updated' | 'failed' | 'cancelled'
interface MailState {
  emails: EmailSummary[]
  loadingList: boolean
  listError: string | null
  selectedEmail: EmailSummary | null
  selectedDetail: EmailDetail | null
  loadingDetail: boolean
  detailError: string | null
  notice: MailNotice | null
}

// The authenticated layout owns the list and detail cache for one session.
export class MailStore {
  private state: MailState = { emails: [], loadingList: false, listError: null, selectedEmail: null, selectedDetail: null, loadingDetail: false, detailError: null, notice: null }
  private listeners = new Set<() => void>()
  private query: MailQuery = {}
  private queryKey = ''
  private listRequest: AbortController | null = null
  private detailRequest: { id: number; controller: AbortController; promise: Promise<void> } | null = null
  private cache = new Map<number, EmailDetail>()
  private noticeTimer: number | undefined
  private disposed = false

  private api: typeof emailApi
  private sessionValid: () => boolean
  constructor(api = emailApi, sessionValid = () => true) {
    this.api = api
    this.sessionValid = sessionValid
  }
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener) } }
  getSnapshot = () => this.state
  private update(patch: Partial<MailState>) {
    if (!this.active()) return
    this.state = { ...this.state, ...patch }
    this.listeners.forEach(listener => listener())
  }
  notify = (message: string, type: 'success' | 'error') => {
    window.clearTimeout(this.noticeTimer)
    this.update({ notice: { message, type } })
    this.noticeTimer = window.setTimeout(() => this.update({ notice: null }), 4000)
  }
  private active() { return !this.disposed && this.sessionValid() }
  setQuery = (query: MailQuery) => {
    const key = JSON.stringify(query)
    if (key === this.queryKey) { void this.reload(); return }
    this.query = query
    this.queryKey = key
    this.update({ emails: [] })
    this.closeDetail()
    void this.reload()
  }
  refresh = async (accountId?: number) => {
    const response = await this.api.refresh(accountId)
    const listResult = this.active() ? await this.reload() : 'cancelled'
    return { ...response, listUpdated: listResult === 'updated' }
  }
  reload = async (): Promise<MailReloadResult> => {
    if (!this.active()) return 'cancelled'
    this.listRequest?.abort()
    const controller = new AbortController()
    this.listRequest = controller
    this.update({ loadingList: true, listError: null })
    try {
      const response = await this.api.list(this.query, controller.signal)
      if (!this.active() || controller.signal.aborted || this.listRequest !== controller) return 'cancelled'
      const emails = response.data || []
      // A combined list contains only the latest 50 across all accounts, so its
      // omissions cannot tell us which other accounts still retain a message.
      if (!this.query.search && this.query.account_id !== undefined) {
        const retained = new Set(emails.map(email => email.id))
        for (const [id, detail] of this.cache) {
          if (detail.account_id === this.query.account_id && !retained.has(id)) this.cache.delete(id)
        }
      }
      emails.forEach(email => {
        const cached = this.cache.get(email.id)
        if (cached) this.cache.set(email.id, { ...cached, ...email })
      })
      const selected = emails.find(email => email.id === this.state.selectedEmail?.id)
      if (this.state.selectedEmail && !selected) this.closeDetail()
      this.update({ emails,
        ...(selected ? { selectedEmail: selected, selectedDetail: this.state.selectedDetail ? { ...this.state.selectedDetail, ...selected } : null } : {}) })
      return 'updated'
    } catch {
      if (!this.active() || controller.signal.aborted || this.listRequest !== controller) return 'cancelled'
      this.update({ listError: '邮件列表加载失败，请重试' })
      return 'failed'
    } finally {
      if (this.listRequest === controller) { this.listRequest = null; this.update({ loadingList: false }) }
    }
  }
  closeDetail = () => {
    this.detailRequest?.controller.abort()
    this.detailRequest = null
    this.update({ selectedEmail: null, selectedDetail: null, loadingDetail: false, detailError: null })
  }
  open = (email: EmailSummary): Promise<void> => {
    if (!this.active()) return Promise.resolve()
    const summary = this.state.emails.find(item => item.id === email.id) || email
    if (this.detailRequest?.id === email.id) return this.detailRequest.promise
    this.detailRequest?.controller.abort()
    this.detailRequest = null
    const cached = this.cache.get(email.id)
    this.update({ selectedEmail: summary, selectedDetail: cached ? { ...cached, ...summary } : null, loadingDetail: !cached, detailError: null })
    if (cached) return Promise.resolve()
    const controller = new AbortController()
    const request = { id: email.id, controller, promise: Promise.resolve() }
    this.detailRequest = request
    request.promise = this.api.get(email.id, controller.signal).then(({ data }) => {
      if (!this.active() || controller.signal.aborted || this.detailRequest !== request) return
      const latest = this.state.emails.find(item => item.id === email.id) || this.state.selectedEmail || summary
      const detail = { ...data, ...latest }
      if (this.cache.size >= 100) this.cache.delete(this.cache.keys().next().value!)
      this.cache.set(email.id, detail)
      this.update({ selectedEmail: detail, selectedDetail: detail, detailError: null })
    }).catch(() => {
      if (!controller.signal.aborted && this.detailRequest === request) this.update({ detailError: '正文加载失败，请重试' })
    }).finally(() => {
      if (this.detailRequest === request) { this.detailRequest = null; this.update({ loadingDetail: false }) }
    })
    return request.promise
  }
  retryDetail = () => this.state.selectedEmail ? this.open(this.state.selectedEmail) : Promise.resolve()
  dispose = () => {
    this.disposed = true
    this.listRequest?.abort()
    this.detailRequest?.controller.abort()
    window.clearTimeout(this.noticeTimer)
    this.cache.clear()
    this.listeners.clear()
  }
}
