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
    if (error.response?.status === 401) {
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
}

export const accountApi = {
  list: () => api.get<{ data: EmailAccount[] }>('/accounts').then(r => r.data),
  add: (data: { email: string; provider: string; username: string; password: string }) =>
    api.post<{ data: EmailAccount }>('/accounts', data).then(r => r.data),
  delete: (id: number) => api.delete(`/accounts/${id}`),
  test: (id: number) => api.post(`/accounts/${id}/test`),
  sync: (id: number) => api.post(`/accounts/${id}/sync`),
}

export const emailApi = {
  list: (params?: { account_id?: number; page?: number; page_size?: number; search?: string }) =>
    api.get<{ data: Email[]; total: number }>('/emails', { params }).then(r => r.data),
  get: (id: number) => api.get<{ data: Email }>(`/emails/${id}`).then(r => r.data),
  markAsRead: (id: number) => api.post(`/emails/${id}/read`),
}

export default api