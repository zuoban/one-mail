import { useMemo, useState } from 'react'
import { emailDocument } from '../mail/sanitize'

export default function EmailBody({ html, light }: { html: string; light: boolean }) {
  const [loadImages, setLoadImages] = useState(false)
  const srcDoc = useMemo(() => emailDocument(html, loadImages, light), [html, loadImages, light])
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2 text-xs" style={{ color: light ? '#525252' : 'var(--text-secondary)' }}>
        <span>{loadImages ? '已允许此邮件加载远程图片' : '已阻止远程图片，保护你的隐私'}</span>
        <button type="button" className="btn btn-secondary btn-sm" onClick={() => setLoadImages(value => !value)}>
          {loadImages ? '阻止远程图片' : '加载此邮件的远程图片'}
        </button>
      </div>
      <iframe
        title="邮件正文"
        sandbox="allow-popups allow-popups-to-escape-sandbox"
        referrerPolicy="no-referrer"
        srcDoc={srcDoc}
        className="block w-full min-h-[55vh] border-0"
      />
    </div>
  )
}
