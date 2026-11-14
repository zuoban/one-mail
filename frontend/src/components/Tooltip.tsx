import { useCallback, useLayoutEffect, useRef, useState } from 'react'
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
  const tooltipRef = useRef<HTMLSpanElement | null>(null)
  const [open, setOpen] = useState(false)
  const [coords, setCoords] = useState<{ left: number; top: number }>({ left: 0, top: 0 })

  const updatePosition = useCallback(() => {
    const node = wrapperRef.current
    const tooltip = tooltipRef.current
    if (!node || !tooltip) return

    const gap = 8
    const margin = 8
    const rect = node.getBoundingClientRect()
    const { width, height } = tooltip.getBoundingClientRect()
    const spaceAbove = rect.top - gap - margin
    const spaceBelow = window.innerHeight - rect.bottom - gap - margin
    const placeBelow = placement === 'bottom'
      ? spaceBelow >= height || spaceBelow >= spaceAbove
      : spaceAbove < height && spaceBelow > spaceAbove
    const top = placeBelow ? rect.bottom + gap : rect.top - gap - height
    const left = rect.left + (rect.width - width) / 2

    setCoords({
      left: Math.max(margin, Math.min(left, window.innerWidth - width - margin)),
      top: Math.max(margin, Math.min(top, window.innerHeight - height - margin)),
    })
  }, [placement])

  useLayoutEffect(() => {
    if (!open) return
    updatePosition()
    window.addEventListener('scroll', updatePosition, true)
    window.addEventListener('resize', updatePosition)
    return () => {
      window.removeEventListener('scroll', updatePosition, true)
      window.removeEventListener('resize', updatePosition)
    }
  }, [open, updatePosition, content])

  return (
    <span
      ref={wrapperRef}
      className={`relative inline-flex ${className}`}
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
    >
      {children}
      {open && createPortal(
        <span
          ref={tooltipRef}
          role="tooltip"
          style={{
            left: `${coords.left}px`,
            top: `${coords.top}px`,
            maxWidth: 'min(360px, calc(100vw - 16px))',
            overflowWrap: 'anywhere',
          }}
          className="tooltip-surface pointer-events-none fixed z-50 w-max rounded-[6px] px-3 py-2 text-xs font-normal whitespace-normal"
        >
          {content}
        </span>,
        document.body,
      )}
    </span>
  )
}
