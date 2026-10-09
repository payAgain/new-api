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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Database } from 'lucide-react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import type { UsageDataset, UsageMetrics } from '../../../usage-types'
import { StatCard } from '../../ui/stat-card'
import { UsageDashboard } from '../usage-dashboard'
import { UsageSummary } from '../usage-summary'

const metrics: UsageMetrics = {
  request_count: 1,
  input_tokens: 1200,
  output_tokens: 300,
  total_tokens: 1500,
  cache_read_tokens: null,
  cache_write_tokens: null,
  cache_ratio: null,
  cache_recorded_requests: 0,
  cache_unknown_requests: 1,
  estimated_requests: 0,
  ambiguous_requests: 0,
}

function usageFixture(): UsageDataset {
  return {
    summary: { ...metrics },
    models: [{ ...metrics, model_name: 'large-context-model' }],
    series: [{ ...metrics, created_at: 1791417600 }],
    meta: {
      start_timestamp: 1791417600,
      end_timestamp: 1791504000,
      granularity: 'hour',
      timezone_offset: 0,
      cache_semantics: 'recorded',
      incomplete: false,
      availability: 'logs',
    },
  }
}

let client: QueryClient
let response: UsageDataset

beforeEach(() => {
  response = usageFixture()
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  useAuthStore.getState().auth.setUser({ id: 1, username: 'owner', role: 10 })
  vi.spyOn(api, 'get').mockImplementation(async () => ({
    data: { success: true, data: response },
  }))
})

afterEach(() => {
  client.clear()
  vi.useRealTimers()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
})

function renderUsage() {
  return render(
    <QueryClientProvider client={client}>
      <UsageDashboard />
    </QueryClientProvider>
  )
}

describe('personal usage dashboard', () => {
  it('names the time-range tablist for assistive technology', () => {
    renderUsage()
    expect(
      screen.getByRole('tablist', { name: 'Usage time range' })
    ).toBeVisible()
  })

  it('keeps compact cache read and write totals visible in the mobile layout', () => {
    // Tailwind's base hidden utility applies below the sm breakpoint.
    render(
      <>
        <style>{'.hidden { display: none; }'}</style>
        <UsageSummary
          summary={{
            ...metrics,
            cache_read_tokens: 240,
            cache_write_tokens: 120,
            cache_ratio: 0.2,
          }}
          loading={false}
          compact
        />
      </>
    )
    const cache = screen.getByRole('region', {
      name: 'Recorded cache share',
    })
    expect(within(cache).getByText('Recorded cache read')).toBeVisible()
    expect(within(cache).getByText('240')).toBeVisible()
    expect(within(cache).getByText('Recorded cache write')).toBeVisible()
    expect(within(cache).getByText('120')).toBeVisible()
  })

  it('preserves the existing mobile visibility of other StatCard details', () => {
    render(
      <>
        <style>{'.hidden { display: none; }'}</style>
        <StatCard
          title='Existing card'
          value='42'
          description='Existing description'
          icon={Database}
          details={[{ label: 'Existing detail', value: '12' }]}
        />
      </>
    )
    expect(screen.getByText('42')).toBeVisible()
    expect(screen.getByText('Existing detail')).not.toBeVisible()
  })

  it('aligns per-model numeric values with their column headings', async () => {
    renderUsage()
    const table = await screen.findByRole('table', { name: 'Usage by model' })
    const headings = within(table).getAllByRole('columnheader').slice(1)
    const values = within(table).getAllByRole('cell').slice(1)
    expect(values).toHaveLength(headings.length)
    for (const cell of [...headings, ...values]) {
      expect(cell).toHaveClass('text-right')
    }
  })

  it('defaults an administrator to personal usage and explicitly switches to all accounts', async () => {
    const user = userEvent.setup()
    renderUsage()
    await screen.findByRole('table', { name: 'Usage by model' })
    expect(api.get).toHaveBeenCalledWith(
      '/api/data/usage/self',
      expect.anything()
    )
    await user.click(screen.getByRole('combobox', { name: 'Usage scope' }))
    await user.click(screen.getByRole('option', { name: 'All accounts' }))
    await waitFor(() =>
      expect(api.get).toHaveBeenCalledWith('/api/data/usage', expect.anything())
    )
    expect(
      screen.getByRole('combobox', { name: 'Usage scope' })
    ).toHaveTextContent('All accounts')
  })

  it('shows unknown cache metadata as not reported without inventing zero-percent hits', async () => {
    renderUsage()
    const table = await screen.findByRole('table', { name: 'Usage by model' })
    expect(
      within(
        screen.getByRole('region', { name: 'Recorded cache share' })
      ).getByText('Not reported')
    ).toBeVisible()
    expect(within(table).getAllByText('Not reported')).toHaveLength(3)
    expect(screen.queryByText('0%')).not.toBeInTheDocument()
    expect(
      await screen.findByText(
        'No cache data was reported for this range. This does not mean a cache miss.'
      )
    ).toBeVisible()
  })

  it('shows recorded zero cache values as zero and keeps input plus output as the total', async () => {
    response.summary = {
      ...metrics,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
      cache_ratio: 0,
      cache_recorded_requests: 1,
      cache_unknown_requests: 0,
    }
    response.models[0] = { ...response.summary, model_name: 'recorded-zero' }
    response.series[0] = { ...response.summary, created_at: 1791417600 }
    renderUsage()
    const table = await screen.findByRole('table', { name: 'Usage by model' })
    expect(
      within(
        screen.getByRole('region', { name: 'Recorded cache share' })
      ).getByText('0%')
    ).toBeVisible()
    expect(within(table).getByText('1,500')).toBeVisible()
    expect(screen.queryByText('Not reported')).not.toBeInTheDocument()
  })

  it('updates range and granularity when the seven-day preset is selected', async () => {
    const user = userEvent.setup()
    renderUsage()
    await screen.findByRole('table', { name: 'Usage by model' })
    await user.click(screen.getByRole('tab', { name: 'Last 7 days' }))
    await waitFor(() =>
      expect(api.get).toHaveBeenLastCalledWith('/api/data/usage/self', {
        params: expect.objectContaining({ granularity: 'day' }),
      })
    )
    const lastCall = vi.mocked(api.get).mock.calls.at(-1)
    const params = lastCall?.[1]?.params
    expect(params.end_timestamp - params.start_timestamp).toBe(7 * 86400)
    expect(screen.getByRole('tab', { name: 'Last 7 days' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
  })

  it('keeps normal users in the self scope without an all-account selector', async () => {
    useAuthStore
      .getState()
      .auth.setUser({ id: 2, username: 'personal', role: 1 })
    renderUsage()
    await screen.findByRole('table', { name: 'Usage by model' })
    expect(
      screen.queryByRole('combobox', { name: 'Usage scope' })
    ).not.toBeInTheDocument()
    expect(api.get).toHaveBeenCalledWith(
      '/api/data/usage/self',
      expect.anything()
    )
  })

  it('shows an empty range without model charts or fabricated cache data', async () => {
    response.summary = {
      ...metrics,
      request_count: 0,
      input_tokens: 0,
      output_tokens: 0,
      total_tokens: 0,
      cache_unknown_requests: 0,
    }
    response.models = []
    response.series = []
    renderUsage()
    expect(
      await screen.findByText('No model usage in this time range')
    ).toBeVisible()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('img', { name: 'Input and output trend' })
    ).not.toBeInTheDocument()
  })

  it('renders a business failure as an error and retries successfully', async () => {
    vi.mocked(api.get).mockResolvedValueOnce({
      data: { success: false, message: 'Logs unavailable' },
    })
    const user = userEvent.setup()
    renderUsage()
    expect(
      await screen.findByText('Model usage could not be loaded')
    ).toBeVisible()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    expect(screen.getByText('Logs unavailable')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Retry' }))
    expect(
      await screen.findByRole('table', { name: 'Usage by model' })
    ).toBeVisible()
  })

  it('preserves an exact model filter and rejects an invalid custom range', async () => {
    const user = userEvent.setup()
    renderUsage()
    await screen.findByRole('table', { name: 'Usage by model' })
    await user.click(screen.getByRole('button', { name: 'Usage filters' }))
    await user.type(
      screen.getByRole('textbox', { name: 'Model name' }),
      'model-v4.1'
    )
    await user.click(screen.getByRole('button', { name: 'Apply filters' }))
    await waitFor(() =>
      expect(api.get).toHaveBeenLastCalledWith('/api/data/usage/self', {
        params: expect.objectContaining({ model_name: 'model-v4.1' }),
      })
    )
    await user.click(screen.getByRole('button', { name: 'Usage filters' }))
    expect(screen.getByRole('textbox', { name: 'Model name' })).toHaveValue(
      'model-v4.1'
    )
    const endInput = screen.getByLabelText('End time')
    fireEvent.change(endInput, { target: { value: '2020-01-01T00:00' } })
    await user.click(screen.getByRole('button', { name: 'Apply filters' }))
    expect(
      await screen.findByText(
        'Choose a time range of up to 31 days with the end after the start.'
      )
    ).toBeVisible()
    expect(screen.getByRole('dialog')).toBeVisible()
  })

  it('bounds long filter badges while preserving exact names for access and lookup', async () => {
    const user = userEvent.setup()
    const modelName = `long-model-${'x'.repeat(245)}`
    const username = 'u'.repeat(64)
    renderUsage()
    await screen.findByRole('table', { name: 'Usage by model' })
    await user.click(screen.getByRole('combobox', { name: 'Usage scope' }))
    await user.click(screen.getByRole('option', { name: 'All accounts' }))
    await user.click(screen.getByRole('button', { name: 'Usage filters' }))
    fireEvent.change(screen.getByRole('textbox', { name: 'Model name' }), {
      target: { value: modelName },
    })
    fireEvent.change(screen.getByRole('textbox', { name: 'Username' }), {
      target: { value: username },
    })
    await user.click(screen.getByRole('button', { name: 'Apply filters' }))
    await waitFor(() =>
      expect(api.get).toHaveBeenLastCalledWith('/api/data/usage', {
        params: expect.objectContaining({ model_name: modelName, username }),
      })
    )
    for (const name of [modelName, username]) {
      const text = screen.getByText(name)
      const badge = text.closest('[data-slot="badge"]')
      expect(badge).toHaveClass('max-w-full', 'min-w-0')
      expect(badge).toHaveAttribute('title', name)
      expect(text).toHaveClass('min-w-0', 'truncate')
      expect(text).toHaveTextContent(name)
    }
  })
  it('refreshes an unchanged range instead of leaving the cached query untouched', async () => {
    const user = userEvent.setup()
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-09T00:00:00Z'))
    renderUsage()
    await screen.findByRole('table', { name: 'Usage by model' })
    vi.mocked(api.get).mockClear()
    await user.click(screen.getByRole('button', { name: 'Refresh usage' }))
    await waitFor(() =>
      expect(api.get).toHaveBeenCalledWith(
        '/api/data/usage/self',
        expect.anything()
      )
    )
  })

  it('resets custom filters and restores the 24-hour preset', async () => {
    vi.mocked(api.get).mockImplementation(async (_url, config) => {
      const data = usageFixture()
      data.models[0].model_name = config?.params?.model_name
        ? 'model-a'
        : 'all-model'
      return { data: { success: true, data } }
    })
    const user = userEvent.setup()
    renderUsage()
    await screen.findByRole('table', { name: 'Usage by model' })
    await user.click(screen.getByRole('button', { name: 'Usage filters' }))
    await user.type(
      screen.getByRole('textbox', { name: 'Model name' }),
      'model-a'
    )
    await user.click(screen.getByRole('button', { name: 'Apply filters' }))
    await waitFor(() =>
      expect(api.get).toHaveBeenLastCalledWith('/api/data/usage/self', {
        params: expect.objectContaining({ model_name: 'model-a' }),
      })
    )
    await user.click(screen.getByRole('button', { name: 'Usage filters' }))
    await user.click(screen.getByRole('button', { name: 'Reset filters' }))
    expect(screen.getByRole('tab', { name: 'Last 24 hours' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    await waitFor(() =>
      expect(
        within(screen.getByRole('table')).getByText('all-model')
      ).toBeVisible()
    )
    await user.click(screen.getByRole('button', { name: 'Usage filters' }))
    expect(screen.getByRole('textbox', { name: 'Model name' })).toHaveValue('')
  })
  it('never displays the all-account results after returning to personal usage', async () => {
    vi.mocked(api.get).mockImplementation(async (url) => {
      const data = usageFixture()
      data.models[0].model_name =
        url === '/api/data/usage'
          ? 'private-all-account-model'
          : 'personal-model'
      return { data: { success: true, data } }
    })
    const user = userEvent.setup()
    renderUsage()
    await screen.findByRole('table', { name: 'Usage by model' })
    await user.click(screen.getByRole('combobox', { name: 'Usage scope' }))
    await user.click(screen.getByRole('option', { name: 'All accounts' }))
    await waitFor(() =>
      expect(
        within(screen.getByRole('table')).getByText('private-all-account-model')
      ).toBeVisible()
    )
    await user.click(screen.getByRole('combobox', { name: 'Usage scope' }))
    await user.click(screen.getByRole('option', { name: 'My usage' }))
    await waitFor(() =>
      expect(
        within(screen.getByRole('table')).getByText('personal-model')
      ).toBeVisible()
    )
    expect(
      screen.queryByText('private-all-account-model')
    ).not.toBeInTheDocument()
  })

  it('identifies estimates and ambiguous input totals instead of presenting them as complete measurements', async () => {
    response.summary.estimated_requests = 1
    response.summary.ambiguous_requests = 1
    renderUsage()
    expect(await screen.findByText('Usage data quality')).toBeVisible()
    expect(
      screen.getByText('1 requests contain estimated token usage.')
    ).toBeVisible()
    expect(
      screen.getByText(
        '1 requests have ambiguous input accounting and are excluded from input and cache totals.'
      )
    ).toBeVisible()
  })
})
