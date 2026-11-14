import DOMPurify from 'dompurify'

const safeStyles: Record<string, true> = {
  color: true, 'background-color': true, 'font-family': true, 'font-size': true,
  'font-weight': true, 'font-style': true, 'text-align': true, 'text-decoration': true,
  'line-height': true, 'letter-spacing': true, 'white-space': true, border: true,
  'border-width': true, 'border-style': true, 'border-color': true, 'border-radius': true,
  'border-collapse': true, 'border-spacing': true, padding: true, 'padding-top': true,
  'padding-right': true, 'padding-bottom': true, 'padding-left': true, margin: true,
  'margin-top': true, 'margin-right': true, 'margin-bottom': true, 'margin-left': true,
  width: true, 'max-width': true, height: true, 'max-height': true,
  'vertical-align': true, display: true, 'list-style-type': true,
}

export function sanitizeEmailHtml(raw: string, loadImages = false): string {
  // Template contents stay inert while resource attributes are removed, before
  // DOMPurify performs its final structural and URL sanitization.
  const template = document.createElement('template')
  template.innerHTML = raw
  const forbiddenTags = ['script', 'style', 'link', 'meta', 'base', 'iframe', 'form', 'input', 'button', 'textarea', 'select', 'option', 'object', 'embed', 'video', 'audio', 'source', 'track', 'canvas', 'svg', 'math', 'template']
  const forbiddenAttributes = ['srcset', 'sizes', 'background', 'ping', 'download', 'autofocus', 'contenteditable', 'id', 'name', 'xlink:href']
  template.content.querySelectorAll(forbiddenTags.join(',')).forEach(element => element.remove())
  template.content.querySelectorAll('*').forEach(element => {
    for (const attribute of Array.from(element.attributes)) {
      if (attribute.name.startsWith('on')) element.removeAttribute(attribute.name)
      if (forbiddenAttributes.includes(attribute.name)) element.removeAttribute(attribute.name)
    }
    const sourceStyle = element.getAttribute('style')
    element.removeAttribute('style')
    if (sourceStyle) {
      const parsed = document.createElement('span').style
      parsed.cssText = sourceStyle
      const clean = document.createElement('span').style
      for (const property of Array.from(parsed)) {
        const value = parsed.getPropertyValue(property)
        if (Object.hasOwn(safeStyles, property) && !/url|expression|var\s*\(|attr\s*\(|image|@|\\|[<>]/i.test(value)
          && !value.replace(/(?:rgb|hsl)a?\([\d\s.,%/+-]*\)/gi, '').includes('(')) {
          clean.setProperty(property, value)
        }
      }
      if (clean.cssText) element.setAttribute('style', clean.cssText)
    }
    if (element.tagName === 'IMG') {
      const src = element.getAttribute('src') || ''
      const rasterData = /^data:image\/(?:png|jpeg|gif|webp);base64,[a-z0-9+/=\s]+$/i.test(src)
      const remote = /^https?:\/\//i.test(src)
      element.removeAttribute('src')
      if (rasterData || (loadImages && remote)) element.setAttribute('src', src)
      element.setAttribute('referrerpolicy', 'no-referrer')
      element.setAttribute('loading', 'lazy')
    } else {
      element.removeAttribute('src')
    }
    if (element.tagName === 'A') {
      const href = element.getAttribute('href') || ''
      if (!/^(https?:\/\/|mailto:)/i.test(href)) element.removeAttribute('href')
      element.setAttribute('target', '_blank')
      element.setAttribute('rel', 'noopener noreferrer')
    } else {
      element.removeAttribute('href')
    }
  })
  return DOMPurify.sanitize(template.innerHTML, {
    USE_PROFILES: { html: true },
    FORBID_TAGS: forbiddenTags,
    FORBID_ATTR: forbiddenAttributes,
    ALLOW_DATA_ATTR: false,
  })
}

export function emailDocument(raw: string, loadImages: boolean, light: boolean): string {
  return prepareEmailDocument(raw, loadImages)(light)
}

export function prepareEmailDocument(raw: string, loadImages: boolean): (light: boolean) => string {
  const sanitized = sanitizeEmailHtml(raw, loadImages)
  const images = loadImages ? 'https: http: data:' : 'data:'
  return (light: boolean) => `<!doctype html><html><head><meta charset="utf-8"><meta name="referrer" content="no-referrer"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src ${images}; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-src 'none'; connect-src 'none'; font-src 'none'; media-src 'none'; object-src 'none'"><style>html{color-scheme:${light ? 'light' : 'dark'}}body{margin:0;padding:0;overflow-wrap:anywhere;font:14px/1.7 -apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:${light ? '#171717' : '#e5e5e5'};background:transparent}img{max-width:100%;height:auto}table{max-width:100%}a{color:${light ? '#2563eb' : '#93c5fd'}}</style></head><body>${sanitized}</body></html>`
}
