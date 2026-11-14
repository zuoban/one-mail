import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import Tooltip from '../src/components/Tooltip'

function mockGeometry(anchor: DOMRect, tooltipWidth = 240, tooltipHeight = 40) {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
    return this.getAttribute('role') === 'tooltip'
      ? new DOMRect(0, 0, tooltipWidth, tooltipHeight)
      : anchor
  })
}

function openTooltip(placement: 'top' | 'bottom' = 'top') {
  render(<Tooltip content="拉取最近 50 封邮件 (r)" placement={placement}><button>刷新</button></Tooltip>)
  fireEvent.mouseEnter(screen.getByRole('button', { name: '刷新' }))
  return screen.getByRole('tooltip')
}

describe('Tooltip viewport boundaries', () => {
  it('flips below a refresh button when there is no room above it', () => {
    const anchor = new DOMRect(300, 12, 64, 36)
    mockGeometry(anchor)
    const tooltip = openTooltip()
    expect(Number.parseFloat(tooltip.style.top)).toBeGreaterThanOrEqual(anchor.bottom + 8)
    expect(Number.parseFloat(tooltip.style.top) + 40).toBeLessThanOrEqual(window.innerHeight - 8)
  })

  it('flips above a button at the bottom even when bottom placement is preferred', () => {
    const anchor = new DOMRect(300, window.innerHeight - 44, 64, 36)
    mockGeometry(anchor)
    const tooltip = openTooltip('bottom')
    expect(Number.parseFloat(tooltip.style.top)).toBeGreaterThanOrEqual(8)
    expect(Number.parseFloat(tooltip.style.top) + 40).toBeLessThanOrEqual(anchor.top - 8)
  })

  it.each([
    ['left', 0],
    ['right', window.innerWidth - 36],
  ])('keeps all text inside the viewport near the %s edge', (_, left) => {
    const anchor = new DOMRect(Number(left), 200, 36, 36)
    mockGeometry(anchor)
    const tooltip = openTooltip()
    const tooltipLeft = Number.parseFloat(tooltip.style.left)
    expect(tooltipLeft).toBeGreaterThanOrEqual(8)
    expect(tooltipLeft + 240).toBeLessThanOrEqual(window.innerWidth - 8)
    expect(Number.parseFloat(tooltip.style.top) + 40).toBeLessThanOrEqual(anchor.top - 8)
  })

  it('follows the anchor when its scroll container moves or the window resizes', () => {
    const anchor = new DOMRect(300, 200, 64, 36)
    mockGeometry(anchor)
    const tooltip = openTooltip()
    anchor.y = 8
    fireEvent.scroll(screen.getByRole('button').parentElement!)
    expect(Number.parseFloat(tooltip.style.top)).toBeGreaterThanOrEqual(anchor.bottom + 8)
    anchor.x = window.innerWidth - 40
    fireEvent.resize(window)
    expect(Number.parseFloat(tooltip.style.left) + 240).toBeLessThanOrEqual(window.innerWidth - 8)
    fireEvent.mouseLeave(screen.getByRole('button'))
    expect(screen.queryByRole('tooltip')).toBeNull()
  })

  it('repositions when updated content needs more vertical space', () => {
    const anchor = new DOMRect(300, 80, 64, 36)
    let tooltipHeight = 40
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return this.getAttribute('role') === 'tooltip' ? new DOMRect(0, 0, 240, tooltipHeight) : anchor
    })
    const { rerender } = render(<Tooltip content="刷新邮件"><button>刷新</button></Tooltip>)
    fireEvent.mouseEnter(screen.getByRole('button'))
    expect(Number.parseFloat(screen.getByRole('tooltip').style.top) + tooltipHeight).toBeLessThanOrEqual(anchor.top - 8)
    tooltipHeight = 120
    rerender(<Tooltip content="邮箱地址、连接状态和上次刷新时间"><button>刷新</button></Tooltip>)
    expect(Number.parseFloat(screen.getByRole('tooltip').style.top)).toBeGreaterThanOrEqual(anchor.bottom + 8)
  })
})
