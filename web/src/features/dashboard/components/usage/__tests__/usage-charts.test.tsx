/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ChartContainer } from '@/components/ui/chart'

import type { UsageSeriesPoint } from '../../../usage-types'
import { UsageCharts, UsageTooltipContent } from '../usage-charts'

const payload = [
  {
    graphicalItemId: 'input',
    name: 'input_tokens',
    dataKey: 'input_tokens',
    value: 1200,
    color: 'var(--chart-1)',
    payload: { created_at: 1791417600 },
  },
  {
    graphicalItemId: 'output',
    name: 'output_tokens',
    dataKey: 'output_tokens',
    value: 300,
    color: 'var(--chart-2)',
    payload: { created_at: 1791417600 },
  },
]

function renderTooltip() {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue(
    new DOMRect(0, 0, 320, 200)
  )
  return render(
    <ChartContainer
      config={{
        input_tokens: { label: 'Input tokens' },
        output_tokens: { label: 'Output tokens' },
      }}
    >
      <div>
        <UsageTooltipContent active label={1791417600} payload={payload} />
      </div>
    </ChartContainer>
  )
}

describe('usage chart tooltip', () => {
  it('formats the timestamp from the series payload when the shared tooltip remaps a numeric label', () => {
    expect(renderTooltip).not.toThrow()
    expect(screen.getByText(/Oct 8/)).toBeVisible()
    expect(screen.queryByText('Invalid Date')).not.toBeInTheDocument()
  })

  it('preserves input and output series names together with formatted token values', () => {
    renderTooltip()
    expect(screen.getByText('Input tokens')).toBeVisible()
    expect(screen.getByText('Output tokens')).toBeVisible()
    expect(screen.getByText('1,200')).toBeVisible()
    expect(screen.getByText('300')).toBeVisible()
  })
})

describe('recorded cache chart', () => {
  it('draws isolated recorded values and zeroes without connecting unknown buckets', async () => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(
      function (this: HTMLElement) {
        return this.classList.contains('recharts-responsive-container')
          ? new DOMRect(0, 0, 640, 256)
          : new DOMRect()
      }
    )
    const emptyBucket: UsageSeriesPoint = {
      created_at: 1791417600,
      request_count: 0,
      input_tokens: 0,
      output_tokens: 0,
      total_tokens: 0,
      cache_read_tokens: null,
      cache_write_tokens: null,
      cache_ratio: null,
      cache_recorded_requests: 0,
      cache_unknown_requests: 0,
      estimated_requests: 0,
      ambiguous_requests: 0,
    }
    const recordedBucket = {
      ...emptyBucket,
      request_count: 1,
      input_tokens: 240,
      output_tokens: 10,
      total_tokens: 250,
      cache_recorded_requests: 1,
    }
    const series: UsageSeriesPoint[] = [
      emptyBucket,
      {
        ...recordedBucket,
        created_at: emptyBucket.created_at + 3600,
        cache_read_tokens: 120,
        cache_write_tokens: 40,
        cache_ratio: 0.5,
      },
      { ...emptyBucket, created_at: emptyBucket.created_at + 7200 },
      {
        ...recordedBucket,
        created_at: emptyBucket.created_at + 10800,
        cache_read_tokens: 160,
        cache_write_tokens: 0,
        cache_ratio: 2 / 3,
      },
      { ...emptyBucket, created_at: emptyBucket.created_at + 14400 },
    ]
    render(<UsageCharts series={series} granularity='hour' cacheOnly />)
    const chart = screen.getByRole('img', { name: 'Recorded cache trend' })
    await waitFor(() =>
      expect(chart.querySelectorAll('.recharts-line')).toHaveLength(2)
    )
    await waitFor(() =>
      expect(chart.querySelectorAll('circle.recharts-line-dot')).toHaveLength(4)
    )
    const lines = chart.querySelectorAll('.recharts-line')
    for (const line of lines) {
      const curve = line.querySelector('.recharts-line-curve')
      expect(curve).toHaveAttribute('d')
      expect(curve?.getAttribute('d')).toMatch(/M/)
      // Only isolated move commands are valid; a line or curve would bridge nulls.
      expect(curve?.getAttribute('d')).not.toMatch(/[LC]/i)
    }
    const dotGroups = chart.querySelectorAll('.recharts-line-dots')
    expect(dotGroups).toHaveLength(2)
    for (const dotGroup of dotGroups) {
      const dots = dotGroup.querySelectorAll('circle.recharts-line-dot')
      expect(dots).toHaveLength(2)
      for (const dot of dots) {
        expect(dot).toBeVisible()
        expect(Number(dot.getAttribute('r'))).toBeGreaterThan(0)
        expect(Number(dot.getAttribute('cx'))).toBeGreaterThan(0)
        expect(Number.isFinite(Number(dot.getAttribute('cy')))).toBe(true)
      }
    }
  })
})
