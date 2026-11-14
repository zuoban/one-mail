export const DEFAULT_EMAIL_LIMIT = 50
export const EMAIL_LIMIT_OPTIONS = [25, DEFAULT_EMAIL_LIMIT, 100, 200] as const
export type EmailLimit = (typeof EMAIL_LIMIT_OPTIONS)[number]
