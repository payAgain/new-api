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
export type UsageScope = 'self' | 'all'
export type UsageGranularity = 'hour' | 'day' | 'month'

export interface UsageMetrics {
  quota: number
  request_count: number
  input_tokens: number
  output_tokens: number
  total_tokens: number
  cache_read_tokens: number | null
  cache_write_tokens: number | null
  cache_ratio: number | null
  cache_recorded_requests: number
  cache_unknown_requests: number
  estimated_requests: number
  ambiguous_requests: number
}

export interface ModelUsage extends UsageMetrics {
  model_name: string
}

export interface UsageSeriesPoint extends UsageMetrics {
  created_at: number
}

export interface UsageDataset {
  summary: UsageMetrics
  models: ModelUsage[]
  series: UsageSeriesPoint[]
  meta: {
    start_timestamp: number
    end_timestamp: number
    granularity: UsageGranularity
    timezone_offset: number
    cache_semantics: 'recorded'
    incomplete: boolean
    availability: 'logs'
  }
}

export interface UsageQueryParams {
  start_timestamp: number
  end_timestamp: number
  granularity: UsageGranularity
  timezone_offset: number
  model_name?: string
  username?: string
}

export interface UsageFilters {
  start: Date
  end: Date
  granularity: UsageGranularity
  model: string
  username: string
}
