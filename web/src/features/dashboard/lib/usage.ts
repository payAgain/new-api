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
import { getRollingDateRange } from '@/lib/time'

import type {
  ModelUsage,
  UsageDataset,
  UsageFilters,
  UsageQueryParams,
  UsageScope,
} from '../usage-types'

export function buildUsageFilters(days = 1): UsageFilters {
  const range = getRollingDateRange(days || 1)
  if (days === 0) range.start = new Date(1000)
  return {
    start: range.start,
    end: range.end,
    granularity: days === 1 ? 'hour' : 'day',
    model: '',
    username: '',
  }
}

export function buildUsageQueryParams(
  filters: UsageFilters,
  scope: UsageScope
): UsageQueryParams {
  const params: UsageQueryParams = {
    start_timestamp: Math.floor(filters.start.getTime() / 1000),
    end_timestamp: Math.floor(filters.end.getTime() / 1000),
    granularity: filters.granularity,
    timezone_offset: filters.end.getTimezoneOffset(),
  }
  if (filters.model.trim()) params.model_name = filters.model.trim()
  if (scope === 'all' && filters.username.trim()) {
    params.username = filters.username.trim()
  }
  return params
}

export function prepareUsageData(data: UsageDataset) {
  const models = [...data.models].sort(
    (a, b) =>
      b.total_tokens - a.total_tokens ||
      a.model_name.localeCompare(b.model_name)
  )
  const series = [...data.series].sort((a, b) => a.created_at - b.created_at)
  // Read and write cache counts describe parts of input and may overlap.
  // They remain separate facts and never increase the total token count.
  return { summary: data.summary, models, series }
}

export function getModelTokenShare(
  model: ModelUsage,
  totalTokens: number
): number {
  if (totalTokens <= 0) return 0
  return Math.min(1, Math.max(0, model.total_tokens / totalTokens))
}
