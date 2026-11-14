import { emailApi, type EmailDetail, type EmailSummary } from '../api'
import { DEFAULT_EMAIL_LIMIT } from './limits'

export interface MailQuery { search?: string; account_id?: number; limit?: number }
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
  private cachedBodyBytes = 0
  private cacheBudgetBytes: number
  private noticeTimer: number | undefined
  private disposed = false

  private api: typeof emailApi
  private sessionValid: () => boolean
  constructor(api = emailApi, sessionValid = () => true, cacheBudgetBytes = 16 * 1024 * 1024) {
    this.api = api
    this.sessionValid = sessionValid
    this.cacheBudgetBytes = cacheBudgetBytes
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
  private bodyBytes(detail: EmailDetail) { return 2 * (detail.body_text.length + detail.body_html.length) }
  private dropCached(id: number) {
    const detail = this.cache.get(id)
    if (!detail) return
    this.cachedBodyBytes -= this.bodyBytes(detail)
    this.cache.delete(id)
  }
  private cacheDetail(detail: EmailDetail) {
    this.dropCached(detail.id)
    const bytes = this.bodyBytes(detail)
    // Large bodies remain readable as the current detail without entering the
    // cross-route cache. UTF-16 code units bound retained body strings, not heap.
    if (bytes > Math.min(this.cacheBudgetBytes, 2 * 1024 * 1024)) return
    while (this.cache.size && (this.cache.size >= 100 || this.cachedBodyBytes + bytes > this.cacheBudgetBytes)) {
      this.dropCached(this.cache.keys().next().value!)
    }
    this.cache.set(detail.id, detail)
    this.cachedBodyBytes += bytes
  }
  invalidateAccount = (accountId: number) => {
    this.listRequest?.abort()
    this.listRequest = null
    for (const [id, detail] of this.cache) if (detail.account_id === accountId) this.dropCached(id)
    if (this.state.selectedEmail?.account_id === accountId) this.closeDetail()
    this.update({ emails: this.state.emails.filter(email => email.account_id !== accountId), loadingList: false })
  }
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
    const response = await this.api.refresh(accountId, this.query.limit)
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
      const emails = response.data
      // Full or combined windows can omit cached messages that still exist.
      // Only an incomplete, unsearched account window is authoritative.
      if (!this.query.search && this.query.account_id !== undefined && emails.length < (this.query.limit ?? DEFAULT_EMAIL_LIMIT)) {
        const retained = new Set(emails.map(email => email.id))
        for (const [id, detail] of this.cache) {
          if (detail.account_id === this.query.account_id && !retained.has(id)) this.dropCached(id)
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
    if (cached) {
      this.cache.delete(email.id)
      this.cache.set(email.id, cached)
    }
    this.update({ selectedEmail: summary, selectedDetail: cached ? { ...cached, ...summary } : null, loadingDetail: !cached, detailError: null })
    if (cached) return Promise.resolve()
    const controller = new AbortController()
    const request = { id: email.id, controller, promise: Promise.resolve() }
    this.detailRequest = request
    request.promise = this.api.get(email.id, controller.signal).then(({ data }) => {
      if (!this.active() || controller.signal.aborted || this.detailRequest !== request) return
      const latest = this.state.emails.find(item => item.id === email.id) || this.state.selectedEmail || summary
      const detail = { ...data, ...latest }
      this.cacheDetail(detail)
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
    this.cachedBodyBytes = 0
    this.listeners.clear()
  }
}
