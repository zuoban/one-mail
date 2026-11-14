import axios from 'axios'
import { emailApi, type EmailDetail, type EmailSummary } from '../api'

export interface MailQuery { search?: string; account_id?: number; unread?: boolean }
export interface MailNotice { message: string; type: 'success' | 'error'; actionLabel?: string; onAction?: () => void }
interface MailState {
  emails: EmailSummary[]
  loadingList: boolean
  listError: string | null
  selectedEmail: EmailSummary | null
  selectedDetail: EmailDetail | null
  loadingDetail: boolean
  detailError: string | null
  selectedIds: Set<number>
  notice: MailNotice | null
  busyBatch: boolean
}

// The authenticated layout owns this store, so route changes do not cancel deletes.
export class MailStore {
  private state: MailState = { emails: [], loadingList: false, listError: null, selectedEmail: null, selectedDetail: null, loadingDetail: false, detailError: null, selectedIds: new Set(), notice: null, busyBatch: false }
  private listeners = new Set<() => void>()
  private query: MailQuery = {}
  private queryKey = ''
  private listRequest: AbortController | null = null
  private detailRequest: { id: number; controller: AbortController; promise: Promise<void> } | null = null
  private cache = new Map<number, EmailDetail>()
  private readOverrides = new Map<number, boolean>()
  private readVersions = new Map<number, number>()
  private readWrites = new Map<number, Promise<void>>()
  private confirmedRead = new Map<number, boolean>()
  private deletes = new Map<number, { timer?: number; controller: AbortController }>()
  private deleted = new Set<number>()
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
    if (this.disposed) return
    this.state = { ...this.state, ...patch }
    this.listeners.forEach(listener => listener())
  }
  notify = (message: string, type: 'success' | 'error', actionLabel?: string, onAction?: () => void) => {
    window.clearTimeout(this.noticeTimer)
    this.update({ notice: { message, type, actionLabel, onAction } })
    this.noticeTimer = window.setTimeout(() => this.update({ notice: null }), actionLabel ? 2200 : 4000)
  }
  dismissNotice = () => this.update({ notice: null })
  private active() { return !this.disposed && this.sessionValid() }
  setQuery = (query: MailQuery) => {
    const key = JSON.stringify(query)
    if (key === this.queryKey) { void this.reload(); return }
    this.query = query
    this.queryKey = key
    this.update({ emails: [], selectedIds: new Set() })
    this.closeDetail()
    void this.reload()
  }
  refresh = async (accountId?: number) => {
    const response = await this.api.refresh(accountId)
    if (this.active()) await this.reload()
    return response
  }
  reload = async () => {
    if (!this.active()) return
    this.listRequest?.abort()
    const controller = new AbortController()
    this.listRequest = controller
    const versionsAtStart = new Map(this.readVersions)
    const writesAtStart = new Set(this.readWrites.keys())
    this.update({ loadingList: true, listError: null })
    try {
      const response = await this.api.list(this.query, controller.signal)
      if (!this.active() || controller.signal.aborted || this.listRequest !== controller) return
      const emails = (response.data || []).filter(email => !this.deletes.has(email.id) && !this.deleted.has(email.id)).map(email => {
        const overlappingWrite = writesAtStart.has(email.id) || this.readWrites.has(email.id) || versionsAtStart.get(email.id) !== this.readVersions.get(email.id)
        const override = overlappingWrite ? this.readOverrides.get(email.id) : undefined
        if (!overlappingWrite) {
          this.readOverrides.delete(email.id)
          this.confirmedRead.set(email.id, email.is_read)
        }
        return override === undefined ? email : { ...email, is_read: override }
      })
      emails.forEach(email => {
        const cached = this.cache.get(email.id)
        if (cached) this.cache.set(email.id, { ...cached, ...email })
      })
      const selected = emails.find(email => email.id === this.state.selectedEmail?.id)
      this.update({ emails, selectedIds: new Set([...this.state.selectedIds].filter(id => emails.some(email => email.id === id))),
        ...(selected ? { selectedEmail: selected, selectedDetail: this.state.selectedDetail ? { ...this.state.selectedDetail, ...selected } : null } : {}) })
    } catch {
      if (!controller.signal.aborted && this.listRequest === controller) this.update({ listError: '邮件列表加载失败，请重试' })
    } finally {
      if (this.listRequest === controller) { this.listRequest = null; this.update({ loadingList: false }) }
    }
  }
  closeDetail = () => {
    this.detailRequest?.controller.abort()
    this.detailRequest = null
    this.update({ selectedEmail: null, selectedDetail: null, loadingDetail: false, detailError: null })
  }
  open = (email: EmailSummary, markRead = true): Promise<void> => {
    if (!this.active() || this.deletes.has(email.id) || this.deleted.has(email.id)) return Promise.resolve()
    const current = this.state.emails.find(item => item.id === email.id) || email
    const override = this.readOverrides.get(email.id)
    const summary = override === undefined ? current : { ...current, is_read: override }
    if (this.detailRequest?.id === email.id) return this.detailRequest.promise
    this.detailRequest?.controller.abort()
    this.detailRequest = null
    const cached = this.cache.get(email.id)
    this.update({ selectedEmail: summary, selectedDetail: cached ? { ...cached, ...summary } : null, loadingDetail: !cached, detailError: null })
    if (markRead && !summary.is_read) void this.setRead(summary, true)
    if (cached) return Promise.resolve()
    const controller = new AbortController()
    const request = { id: email.id, controller, promise: Promise.resolve() }
    this.detailRequest = request
    request.promise = this.api.get(email.id, controller.signal).then(({ data }) => {
      if (!this.active() || controller.signal.aborted || this.detailRequest !== request) return
      const latest = this.state.emails.find(item => item.id === email.id) || this.state.selectedEmail || summary
      const desired = this.readOverrides.get(email.id)
      const detail = { ...data, ...latest, is_read: desired ?? data.is_read }
      if (this.cache.size >= 100) this.cache.delete(this.cache.keys().next().value!)
      this.cache.set(email.id, detail)
      this.update({ emails: this.state.emails.map(item => item.id === email.id ? { ...item, is_read: detail.is_read } : item), selectedEmail: detail, selectedDetail: detail, detailError: null })
    }).catch(() => {
      if (!controller.signal.aborted && this.detailRequest === request) this.update({ detailError: '正文加载失败，请重试' })
    }).finally(() => {
      if (this.detailRequest === request) { this.detailRequest = null; this.update({ loadingDetail: false }) }
    })
    return request.promise
  }
  retryDetail = () => this.state.selectedEmail ? this.open(this.state.selectedEmail, false) : Promise.resolve()
  private patchRead(id: number, is_read: boolean) {
    const cached = this.cache.get(id)
    if (cached) this.cache.set(id, { ...cached, is_read })
    this.update({ emails: this.state.emails.map(email => email.id === id ? { ...email, is_read } : email),
      selectedEmail: this.state.selectedEmail?.id === id ? { ...this.state.selectedEmail, is_read } : this.state.selectedEmail,
      selectedDetail: this.state.selectedDetail?.id === id ? { ...this.state.selectedDetail, is_read } : this.state.selectedDetail })
  }
  setRead = async (email: EmailSummary, desired: boolean) => {
    if (!this.active()) return
    const previous = this.readOverrides.get(email.id) ?? this.state.emails.find(item => item.id === email.id)?.is_read ?? email.is_read
    if (!this.readWrites.has(email.id)) this.confirmedRead.set(email.id, previous)
    if (previous === desired && !this.readWrites.has(email.id)) return
    const version = (this.readVersions.get(email.id) || 0) + 1
    this.readVersions.set(email.id, version)
    this.readOverrides.set(email.id, desired)
    const prior = this.readWrites.get(email.id) || Promise.resolve()
    const write = prior.catch(() => {}).then(async () => {
      if (!this.active()) return
      const response = desired ? await this.api.markAsRead(email.id) : await this.api.markAsUnread(email.id)
      this.confirmedRead.set(email.id, desired)
      if (this.active() && response.sync_pending) this.notify('已保存，等待同步', 'success')
    })
    this.readWrites.set(email.id, write)
    this.patchRead(email.id, desired)
    try { await write } catch (error) {
      if (this.active() && this.readVersions.get(email.id) === version) {
        const confirmed = this.confirmedRead.get(email.id) ?? previous
        this.readOverrides.set(email.id, confirmed)
        this.patchRead(email.id, confirmed)
        if (axios.isAxiosError(error) && error.response?.status === 409) {
          this.cache.clear()
          this.closeDetail()
          this.notify('邮箱已变化，已刷新邮件列表，请重试', 'error')
        } else {
          this.notify('已读状态保存失败，已恢复原状态', 'error')
        }
        void this.reload()
      }
    } finally {
      if (this.readWrites.get(email.id) === write) this.readWrites.delete(email.id)
    }
  }
  setSelectedIds = (ids: Set<number> | ((previous: Set<number>) => Set<number>)) => {
    if (this.state.busyBatch) return
    const next = typeof ids === 'function' ? ids(this.state.selectedIds) : ids
    this.update({ selectedIds: new Set([...next].filter(id => this.state.emails.some(email => email.id === id))) })
  }
  private remove(ids: Set<number>) {
    ids.forEach(id => this.cache.delete(id))
    this.update({ emails: this.state.emails.filter(email => !ids.has(email.id)), selectedIds: new Set([...this.state.selectedIds].filter(id => !ids.has(id))) })
    if (this.state.selectedEmail && ids.has(this.state.selectedEmail.id)) this.closeDetail()
  }
  scheduleDelete = (email: EmailSummary) => {
    if (!this.active() || this.deletes.has(email.id)) return
    const scope = this.queryKey
    const entry: { controller: AbortController; timer?: number } = { controller: new AbortController() }
    this.deletes.set(email.id, entry)
    this.remove(new Set([email.id]))
    entry.timer = window.setTimeout(async () => {
      entry.timer = undefined
      if (!this.active()) { this.deletes.delete(email.id); return }
      try {
        await this.api.delete(email.id, entry.controller.signal)
        this.deleted.add(email.id)
      } catch {
        if (this.active()) {
          if (scope === this.queryKey && !this.state.emails.some(item => item.id === email.id)) this.update({ emails: [...this.state.emails, { ...email, is_read: this.readOverrides.get(email.id) ?? email.is_read }] })
          this.notify('删除失败，请重试', 'error')
        }
      } finally {
        this.deletes.delete(email.id)
        if (this.active()) void this.reload()
      }
    }, 2200)
    this.notify('已删除邮件', 'success', '撤销', () => {
      if (entry.timer === undefined || !this.active()) return
      window.clearTimeout(entry.timer)
      this.deletes.delete(email.id)
      if (scope === this.queryKey && !this.state.emails.some(item => item.id === email.id)) this.update({ emails: [...this.state.emails, { ...email, is_read: this.readOverrides.get(email.id) ?? email.is_read }] })
      this.notify('已撤销删除', 'success')
      void this.reload()
    })
  }
  batchDelete = async () => {
    if (this.state.busyBatch || !this.active()) return
    const ids = [...this.state.selectedIds]
    if (!ids.length) return
    const scope = this.queryKey
    const originals = this.state.emails.filter(email => this.state.selectedIds.has(email.id))
    this.update({ busyBatch: true })
    // Hide in-flight deletions from concurrent list refreshes too.
    ids.forEach(id => this.deletes.set(id, { controller: new AbortController() }))
    const results = await Promise.allSettled(ids.map(id => this.api.delete(id, this.deletes.get(id)!.controller.signal)))
    if (!this.active()) return
    const failed = new Set<number>()
    const succeeded = new Set<number>()
    ids.forEach((id, index) => {
      this.deletes.delete(id)
      if (results[index].status === 'fulfilled') { succeeded.add(id); this.deleted.add(id) }
      else failed.add(id)
    })
    this.remove(succeeded)
    const restored = scope === this.queryKey ? originals.filter(email => failed.has(email.id) && !this.state.emails.some(item => item.id === email.id)) : []
    this.update({ busyBatch: false, emails: [...this.state.emails, ...restored], selectedIds: scope === this.queryKey ? failed : this.state.selectedIds })
    this.notify(failed.size ? `已删除 ${succeeded.size} 封，${failed.size} 封删除失败，请重试` : `已删除 ${succeeded.size} 封邮件`, failed.size ? 'error' : 'success')
    await this.reload()
  }
  batchRead = async () => {
    if (this.state.busyBatch || !this.active()) return
    const emails = this.state.emails.filter(email => this.state.selectedIds.has(email.id))
    const scope = this.queryKey
    this.update({ busyBatch: true })
    await Promise.all(emails.map(email => this.setRead(email, true)))
    const failed = new Set(emails.filter(email => !this.state.emails.find(item => item.id === email.id)?.is_read).map(email => email.id))
    const pending = this.state.notice?.message === '已保存，等待同步'
    this.update({ busyBatch: false, selectedIds: scope === this.queryKey ? failed : this.state.selectedIds })
    if (!failed.size && !pending) this.notify(`已将 ${emails.length} 封标为已读`, 'success')
  }
  dispose = () => {
    this.disposed = true
    this.listRequest?.abort()
    this.detailRequest?.controller.abort()
    this.deletes.forEach(entry => { window.clearTimeout(entry.timer); entry.controller.abort() })
    this.deletes.clear()
    window.clearTimeout(this.noticeTimer)
    this.listeners.clear()
  }
}
