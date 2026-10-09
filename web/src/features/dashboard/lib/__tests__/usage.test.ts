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
import { describe, expect, it } from 'vitest'

import type {
  UsageDataset,
  UsageFilters,
  UsageMetrics,
} from '../../usage-types'
import {
  buildUsageQueryParams,
  getModelTokenShare,
  prepareUsageData,
} from '../usage'

const metrics: UsageMetrics = {
  quota: 0,
  request_count: 1,
  input_tokens: 100,
  output_tokens: 20,
  total_tokens: 120,
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
    summary: {
      ...metrics,
      input_tokens: 300,
      output_tokens: 40,
      total_tokens: 340,
      cache_read_tokens: 90,
      cache_write_tokens: 60,
      cache_ratio: 0.3,
    },
    models: [
      { ...metrics, model_name: 'many-small-requests', request_count: 100 },
      {
        ...metrics,
        model_name: 'large-context',
        request_count: 1,
        input_tokens: 200,
        output_tokens: 20,
        total_tokens: 220,
        cache_read_tokens: 90,
        cache_write_tokens: 60,
        cache_ratio: 0.45,
      },
    ],
    series: [
      { ...metrics, created_at: 2000 },
      { ...metrics, created_at: 1000 },
    ],
    meta: {
      start_timestamp: 1000,
      end_timestamp: 3000,
      granularity: 'hour',
      timezone_offset: 0,
      cache_semantics: 'recorded',
      incomplete: false,
      availability: 'logs',
    },
  }
}

const filters: UsageFilters = {
  start: new Date('2026-10-08T00:00:00Z'),
  end: new Date('2026-10-09T00:00:00Z'),
  granularity: 'hour',
  model: ' model-a ',
  username: ' other-user ',
}

describe('personal model usage data', () => {
  it('ranks models by total tokens rather than request counts and keeps chronological series without mutating the response', () => {
    const data = usageFixture()
    const original = structuredClone(data)
    const result = prepareUsageData(data)
    expect(result.models.map((model) => model.model_name)).toEqual([
      'large-context',
      'many-small-requests',
    ])
    expect(result.series.map((point) => point.created_at)).toEqual([1000, 2000])
    expect(data).toEqual(original)
  })

  it('keeps cache counts as input facts and never adds them to total tokens', () => {
    const result = prepareUsageData(usageFixture())
    expect(result.summary.total_tokens).toBe(340)
    expect(result.summary.input_tokens + result.summary.output_tokens).toBe(340)
    expect(result.summary.cache_read_tokens).toBe(90)
    expect(result.summary.cache_write_tokens).toBe(60)
  })

  it('preserves missing cache metadata as null and recorded zero as zero', () => {
    const data = usageFixture()
    data.models[1] = {
      ...data.models[1],
      cache_read_tokens: 0,
      cache_write_tokens: 0,
      cache_ratio: 0,
    }
    const result = prepareUsageData(data)
    expect(result.models[0].cache_read_tokens).toBe(0)
    expect(result.models[0].cache_ratio).toBe(0)
    expect(result.models[1].cache_read_tokens).toBeNull()
    expect(result.models[1].cache_ratio).toBeNull()
  })

  it('excludes username filters from the self scope and preserves exact model names and browser timezone', () => {
    const selfParams = buildUsageQueryParams(filters, 'self')
    expect(selfParams).toEqual({
      start_timestamp: 1791417600,
      end_timestamp: 1791504000,
      granularity: 'hour',
      timezone_offset: filters.end.getTimezoneOffset(),
      model_name: 'model-a',
    })
    expect(buildUsageQueryParams(filters, 'all').username).toBe('other-user')
  })

  it('returns no model share for an empty range and uses total-token share for a populated range', () => {
    const model = usageFixture().models[1]
    expect(getModelTokenShare(model, 0)).toBe(0)
    expect(getModelTokenShare(model, 440)).toBe(0.5)
    const data = usageFixture()
    data.models = []
    data.series = []
    expect(prepareUsageData(data).models).toEqual([])
    expect(prepareUsageData(data).series).toEqual([])
  })
})
