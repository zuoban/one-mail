import { useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { createPortal } from 'react-dom'

interface TooltipProps {
  content: ReactNode
  children: ReactNode
  className?: string
  placement?: 'top' | 'bottom'
}

export default function Tooltip({ content, children, className = '', placement = 'top' }: TooltipProps) {
  const wrapperRef = useRef<HTMLSpanElement | null>(null)
  const [open, setOpen] = useState(false)
  const [coords, setCoords] = useState<{ left: number; top: number }>({ left: 0, top: 0 })

  const updatePosition = () => {
    const node = wrapperRef.current
    if (!node) return
    const rect = node.getBoundingClientRect()
    const top = placement === 'bottom'
      ? rect.bottom + 8
      : rect.top - 8
    setCoords({
      left: rect.left + rect.width / 2,
      top,
    })
  }

  useEffect(() => {
    if (!open) return
    updatePosition()
    const handle = () => updatePosition()
    window.addEventListener('scroll', handle, true)
    window.addEventListener('resize', handle)
    return () => {
      window.removeEventListener('scroll', handle, true)
      window.removeEventListener('resize', handle)
    }
  }, [open, placement])

  return (
    <span
      ref={wrapperRef}
      className={`relative inline-flex ${className}`}
      onMouseEnter={() => {
        setOpen(true)
        updatePosition()
      }}
      onMouseLeave={() => setOpen(false)}
    >
      {children}
      {open && createPortal(
        <span
          role="tooltip"
          style={{
            left: `${coords.left}px`,
            top: `${coords.top}px`,
          }}
          className={`pointer-events-none fixed z-50 w-max max-w-[360px] -translate-x-1/2 rounded-lg bg-slate-900/90 px-2.5 py-1.5 text-[11px] font-medium text-white shadow-lg whitespace-nowrap ${
            placement === 'bottom' ? '' : '-translate-y-full'
          }`}
        >
          {content}
          <span
            className={`absolute left-1/2 h-0 w-0 -translate-x-1/2 border-[6px] border-transparent ${
              placement === 'bottom'
                ? 'top-0 -translate-y-1/2 border-b-slate-900/90'
                : 'bottom-0 translate-y-1/2 border-t-slate-900/90'
            }`}
          />
        </span>,
        document.body,
      )}
    </span>
  )
}
