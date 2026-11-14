import axios from 'axios'

const api = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || '/api',
  timeout: 30000,
})

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

export const SESSION_EXPIRED_EVENT = 'one-mail:session-expired'

api.interceptors.response.use(
  (response) => response,
  (error) => {
    const isAuthEndpoint = error.config?.url?.includes('/auth/')
    if (error.response?.status === 401 && !isAuthEndpoint) {
      const authorization = error.config?.headers?.Authorization
      const currentToken = localStorage.getItem('token')
      if (authorization === (currentToken ? `Bearer ${currentToken}` : undefined)) {
        localStorage.removeItem('token')
        window.dispatchEvent(new Event(SESSION_EXPIRED_EVENT))
      }
    }
    return Promise.reject(error)
  }
)

export interface AuthStatus {
  password_required: boolean
  authenticated: boolean
}

export interface EmailAccount {
  id: number
  email: string
  provider: string
  username: string
  status: string
  last_sync_time: string
  color: string
}

export interface EmailSummary {
  id: number
  created_at: string
  account_id: number
  from: string
  from_name: string
  to: string
  subject: string
  date: string
  has_attachment: boolean
  is_read: boolean
  folder: string
}

export interface EmailDetail extends EmailSummary {
  body_text: string
  body_html: string
}

export interface EmailListResponse {
  data: EmailSummary[]
  total: number
}

export interface RefreshEmailsResponse {
  message: string
  refreshed_accounts?: number
  new_count: number
  errors?: Record<string, string>
}
export interface TelegramConfig {
  enabled: boolean
  bot_token: string
  chat_id: string
}

export interface ReadStatusResponse {
  message: string
  count?: number
  sync_pending: boolean
}

export const authApi = {
  status: () => api.get<AuthStatus>('/auth/status').then(r => r.data),
  login: (data: { password: string }) =>
    api.post<{ token: string }>('/auth/login', data).then(r => r.data),
}

export const accountApi = {
  list: () => api.get<{ data: EmailAccount[] }>('/accounts').then(r => r.data),
  add: (data: { email: string; provider: string; username: string; password: string; color?: string }) =>
    api.post<{ data: EmailAccount }>('/accounts', data).then(r => r.data),
  update: (id: number, data: { email?: string; provider?: string; username?: string; password?: string; color?: string }) =>
    api.put<{ data: EmailAccount }>(`/accounts/${id}`, data).then(r => r.data),
  delete: (id: number) => api.delete(`/accounts/${id}`),
  test: (id: number) => api.post(`/accounts/${id}/test`),
  refresh: (id: number) => api.post<RefreshEmailsResponse>(`/accounts/${id}/refresh`, null, { timeout: 45000 }).then(r => r.data),
}

export const emailApi = {
  list: (
    params?: {
      account_id?: number
      search?: string
      unread?: boolean
    },
    signal?: AbortSignal,
  ) => api.get<EmailListResponse>('/emails', { params, signal }).then(r => r.data),
  refresh: (accountId?: number) =>
    api.post<RefreshEmailsResponse>('/emails/refresh', null, {
      params: accountId ? { account_id: accountId } : undefined,
      timeout: 45000,
    }).then(r => r.data),
  get: (id: number, signal?: AbortSignal) => api.get<{ data: EmailDetail }>(`/emails/${id}`, { signal }).then(r => r.data),
  markAsRead: (id: number) => api.post<ReadStatusResponse>(`/emails/${id}/read`).then(r => r.data),
  markAsUnread: (id: number) => api.post<ReadStatusResponse>(`/emails/${id}/unread`).then(r => r.data),
  batchMarkAsRead: (ids: number[]) => api.post<ReadStatusResponse>('/emails/batch/read', { ids }).then(r => r.data),
  delete: (id: number, signal?: AbortSignal) => api.delete(`/emails/${id}`, { signal }),
}
export const telegramApi = {
  getConfig: () => api.get<{ data: TelegramConfig }>('/telegram/config').then(r => r.data),
  updateConfig: (data: { enabled?: boolean; bot_token?: string; chat_id?: string }) =>
    api.put<{ data: TelegramConfig }>('/telegram/config', data).then(r => r.data),
  testConnection: (data?: { bot_token?: string; chat_id?: string }) =>
    api.post<{ message: string }>('/telegram/test', data || {}).then(r => r.data),
  sendEmail: (emailId: number) =>
    api.post<{ message: string }>(`/telegram/send/${emailId}`).then(r => r.data),
}
export default api
