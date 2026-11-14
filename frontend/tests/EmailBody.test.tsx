import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import EmailBody from '../src/components/EmailBody'
import { emailDocument, sanitizeEmailHtml } from '../src/mail/sanitize'
import { ReadingPreferencesProvider } from '../src/context/ReadingPreferencesContext'

function parsed(html: string) { return new DOMParser().parseFromString(html, 'text/html') }

const attack = `<script>parent.localStorage.getItem('token')</script>
  <iframe src="https://tracker.example/frame"></iframe><form action="https://tracker.example"><input autofocus name="password"></form>
  <object data="https://tracker.example/object"></object><svg onload="alert(1)"><a href="javascript:alert(1)">bad</a></svg>
  <style>@import 'https://tracker.example/css';body{background:url(https://tracker.example/bg)}</style>
  <link rel="stylesheet" href="https://tracker.example/css"><meta http-equiv="refresh" content="0;url=https://tracker.example">
  <img src="https://tracker.example/pixel" srcset="https://tracker.example/2x 2x" onerror="alert(1)" style="background-image:url(https://tracker.example/bg)">
  <div onclick="alert(1)" style="position:fixed;inset:0;background:url(https://tracker.example/css);color:rgb(1, 2, 3);padding:8px">正文</div>
  <a href="javascript:alert(1)">危险链接</a><a href="https://example.com/read" ping="https://tracker.example/ping">安全链接</a>`

describe('isolated HTML mail', () => {
  it('removes active content, dangerous URLs and every automatic remote resource', () => {
    const doc = parsed(sanitizeEmailHtml(attack))
    expect(doc.querySelector('script,iframe,form,input,object,embed,svg,style,link,meta')).toBeNull()
    expect(doc.querySelector('img')?.hasAttribute('src')).toBe(false)
    expect(doc.querySelector('img')?.hasAttribute('srcset')).toBe(false)
    expect(doc.querySelector('a')?.hasAttribute('href')).toBe(false)
    for (const element of doc.body.querySelectorAll('*')) {
      for (const attribute of element.attributes) {
        expect(attribute.name.startsWith('on')).toBe(false)
        expect(attribute.value).not.toMatch(/url\s*\(|javascript:/i)
      }
    }
    const content = doc.querySelector('div')!
    expect(content.style.position).toBe('')
    expect(content.style.backgroundImage).toBe('')
    expect(content.style.padding).toBe('8px')
    expect(content.textContent).toBe('正文')
    expect(doc.querySelector('a[href]')?.getAttribute('rel')).toBe('noopener noreferrer')
  })

  it('only opts image sources into loading, while CSS resources remain blocked', () => {
    const doc = parsed(emailDocument(`${attack}<img src="data:image/svg+xml;base64,PHN2Zz4="><img src="/api/private">`, true, true))
    expect(doc.querySelector('img')?.getAttribute('src')).toBe('https://tracker.example/pixel')
    expect([...doc.querySelectorAll('img')].slice(1).every(image => !image.hasAttribute('src'))).toBe(true)
    expect(doc.querySelector('div')?.style.backgroundImage).toBe('')
    const policy = doc.querySelector('meta[http-equiv="Content-Security-Policy"]')?.getAttribute('content')
    expect(policy).toContain("default-src 'none'")
    expect(policy).toContain("connect-src 'none'")
    expect(policy).toContain('img-src https: http: data:')
  })

  it('loads remote images by default in an opaque sandbox without exposing active content or session credentials', () => {
    localStorage.setItem('token', 'private-session-secret')
    render(<EmailBody key={1} html={attack} light />, { wrapper: ReadingPreferencesProvider })
    const frame = screen.getByTitle<HTMLIFrameElement>('邮件正文')
    const permissions = frame.getAttribute('sandbox')?.split(' ')
    expect(permissions).not.toContain('allow-scripts')
    expect(permissions).not.toContain('allow-same-origin')
    expect(permissions).not.toContain('allow-forms')
    expect(frame.getAttribute('referrerpolicy')).toBe('no-referrer')
    expect(frame.srcdoc).not.toContain('private-session-secret')
    const doc = parsed(frame.srcdoc)
    expect(doc.querySelector('img')?.getAttribute('src')).toBe('https://tracker.example/pixel')
    expect(doc.body.querySelector('script,iframe,form,input,object,embed,svg,style,link')).toBeNull()
    expect(doc.querySelector('img')?.hasAttribute('onerror')).toBe(false)
    expect(screen.queryByRole('button', { name: '加载此邮件的远程图片' })).toBeNull()
  })

  it('keeps temporary image permission scoped to one email when the saved preference blocks images', () => {
    localStorage.setItem('blockRemoteImages', 'true')
    const { rerender } = render(<EmailBody key={1} html={attack} light />, { wrapper: ReadingPreferencesProvider })
    const frame = screen.getByTitle<HTMLIFrameElement>('邮件正文')
    expect(parsed(frame.srcdoc).querySelector('img')?.hasAttribute('src')).toBe(false)
    expect(frame.srcdoc).toContain('img-src data:;')
    fireEvent.click(screen.getByRole('button', { name: '加载此邮件的远程图片' }))
    expect(parsed(frame.srcdoc).querySelector('img')?.getAttribute('src')).toBe('https://tracker.example/pixel')
    expect(localStorage.getItem('blockRemoteImages')).toBe('true')
    rerender(<EmailBody key={2} html={attack} light />)
    expect(parsed(screen.getByTitle<HTMLIFrameElement>('邮件正文').srcdoc).querySelector('img')?.hasAttribute('src')).toBe(false)
    fireEvent.click(screen.getByRole('button', { name: '加载此邮件的远程图片' }))
    fireEvent.click(screen.getByRole('button', { name: '阻止远程图片' }))
    expect(parsed(screen.getByTitle<HTMLIFrameElement>('邮件正文').srcdoc).querySelector('img')?.hasAttribute('src')).toBe(false)
  })
})
