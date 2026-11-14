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

api.interceptors.response.use(
  (response) => response,
  (error) => {
    const isAuthEndpoint = error.config?.url?.includes('/auth/')
    if (error.response?.status === 401 && !isAuthEndpoint) {
      localStorage.removeItem('token')
      window.location.href = '/login'
    }
    return Promise.reject(error)
  }
)

export interface User {
  id: number
  username: string
  email: string
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

export interface Email {
  id: number
  created_at: string
  account_id: number
  from: string
  from_name: string
  to: string
  subject: string
  date: string
  body_text?: string
  body_html?: string
  has_attachment: boolean
  is_read: boolean
  folder: string
}

export interface EmailListResponse {
  data: Email[]
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

export interface AuthResponse {
  token: string
  user: User
}

export const authApi = {
  login: (data: { username: string; password: string }) =>
    api.post<AuthResponse>('/auth/login', data).then(r => r.data),
  register: (data: { username: string; password: string; email: string }) =>
    api.post<AuthResponse>('/auth/register', data).then(r => r.data),
  me: () => api.get<{ data: User }>('/auth/me').then(r => r.data),
  updateProfile: (data: { username?: string; email?: string }) =>
    api.put<{ data: User }>('/auth/profile', data).then(r => r.data),
  changePassword: (data: { old_password: string; new_password: string }) =>
    api.put<{ message: string }>('/auth/password', data).then(r => r.data),
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
  get: (id: number) => api.get<{ data: Email }>(`/emails/${id}`).then(r => r.data),
  markAsRead: (id: number) => api.post(`/emails/${id}/read`),
  markAsUnread: (id: number) => api.post(`/emails/${id}/unread`),
  batchMarkAsRead: (ids: number[]) => api.post<{ message: string; count: number }>('/emails/batch/read', { ids }).then(r => r.data),
  delete: (id: number) => api.delete(`/emails/${id}`),
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
