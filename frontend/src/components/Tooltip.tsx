import { useCallback, useEffect, useRef, useState } from 'react'
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

  const updatePosition = useCallback(() => {
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
  }, [placement])

  useEffect(() => {
    if (!open) return
    updatePosition()
    window.addEventListener('scroll', updatePosition, true)
    window.addEventListener('resize', updatePosition)
    return () => {
      window.removeEventListener('scroll', updatePosition, true)
      window.removeEventListener('resize', updatePosition)
    }
  }, [open, updatePosition])

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
          className={`tooltip-surface pointer-events-none fixed z-50 w-max max-w-[360px] -translate-x-1/2 rounded-[6px] px-2.5 py-1 text-xs font-normal whitespace-nowrap ${
            placement === 'bottom' ? '' : '-translate-y-full'
          }`}
        >
          {content}
        </span>,
        document.body,
      )}
    </span>
  )
}
