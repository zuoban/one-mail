import axios from 'axios'

const api = axios.create({
  baseURL: 'http://localhost:8080/api',
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
    // 登录/注册接口的 401 是正常业务错误，不应跳转
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
  default_sync_interval: number
  default_sync_folders: string
  default_enable_auto_sync: boolean
}

export interface EmailAccount {
  id: number
  email: string
  provider: string
  username: string
  status: string
  last_sync_time: string
  color: string
  sync_folders: string
  enable_auto_sync: boolean
}

export interface FolderStatus {
  name: string
  messages: number
  unseen: number
}

export interface SyncStatus {
  account_id: number
  running: boolean
  last_sync_time: string
  error: string
  new_count: number
}

export interface SyncLog {
  id: number
  created_at: string
  account_id: number
  start_time: string
  end_time: string
  status: string
  new_count: number
  error: string
  duration_ms: number
}

export interface Email {
  id: number
  account_id: number
  from: string
  from_name: string
  to: string
  subject: string
  date: string
  body_text: string
  body_html: string
  has_attachment: boolean
  is_read: boolean
  folder: string
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
  sync: (id: number) => api.post(`/accounts/${id}/sync`),
  listFolders: (id: number) => api.get<{ data: FolderStatus[] }>(`/accounts/${id}/folders`).then(r => r.data),
  updateSyncConfig: (id: number, data: { sync_interval?: number; sync_folders?: string; enable_auto_sync?: boolean }) =>
    api.put(`/accounts/${id}/sync/config`, data),
}

export const syncApi = {
  getStatus: (id: number) => api.get<{ data: SyncStatus }>(`/sync/status/${id}`).then(r => r.data),
  getAllStatuses: () => api.get<{ data: Record<string, SyncStatus> }>('/sync/status').then(r => r.data),
  start: () => api.post('/sync/start'),
  stop: () => api.post('/sync/stop'),
  getLogs: (params?: { account_id?: number; page?: number; page_size?: number }) =>
    api.get<{ data: SyncLog[]; total: number; page: number; page_size: number }>('/sync/logs', { params }).then(r => r.data),
  getLogsByAccount: (accountId: number, params?: { page?: number; page_size?: number }) =>
    api.get<{ data: SyncLog[]; total: number; page: number; page_size: number }>(`/sync/logs/${accountId}`, { params }).then(r => r.data),
  clearLogs: (accountId: number) => api.delete(`/sync/logs/${accountId}`),
  applyToAll: () => api.post<{ message: string; updated_count: number }>('/sync/apply-to-all').then(r => r.data),
}

export const emailApi = {
  list: (params?: { account_id?: number; page?: number; page_size?: number; search?: string }) =>
    api.get<{ data: Email[]; total: number }>('/emails', { params }).then(r => r.data),
  get: (id: number) => api.get<{ data: Email }>(`/emails/${id}`).then(r => r.data),
  markAsRead: (id: number) => api.post(`/emails/${id}/read`),
  markAsUnread: (id: number) => api.post(`/emails/${id}/unread`),
  delete: (id: number) => api.delete(`/emails/${id}`),
}

export default api
