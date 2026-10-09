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
import type { ComponentProps } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Area,
  AreaChart,
  CartesianGrid,
  Line,
  LineChart,
  XAxis,
  YAxis,
} from 'recharts'

import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
} from '@/components/ui/chart'
import { toIntlLocale } from '@/i18n/languages'
import { formatCompactNumber, formatNumber } from '@/lib/format'

import type { UsageGranularity, UsageSeriesPoint } from '../../usage-types'
import { PanelWrapper } from '../ui/panel-wrapper'

interface UsageChartsProps {
  series: UsageSeriesPoint[]
  granularity: UsageGranularity
  cacheOnly?: boolean
}

export function UsageCharts(props: UsageChartsProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const dateFormatter = new Intl.DateTimeFormat(
    locale,
    props.granularity === 'hour'
      ? { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }
      : { month: '2-digit', day: '2-digit' }
  )
  const config = {
    input_tokens: { label: t('Input tokens'), color: 'var(--chart-1)' },
    output_tokens: { label: t('Output tokens'), color: 'var(--chart-2)' },
    cache_read_tokens: {
      label: t('Recorded cache read'),
      color: 'var(--chart-3)',
    },
    cache_write_tokens: {
      label: t('Recorded cache write'),
      color: 'var(--chart-4)',
    },
  }
  const title = props.cacheOnly
    ? t('Recorded cache trend')
    : t('Input and output trend')
  const description = props.cacheOnly
    ? t(
        'Cache is part of input. Read and write lines are shown separately and are not stacked.'
      )
    : t(
        'Total tokens = input + output. Cache counts are already part of input.'
      )
  const hasCache = props.series.some(
    (point) =>
      point.cache_read_tokens !== null || point.cache_write_tokens !== null
  )
  const axis = (
    <>
      <CartesianGrid vertical={false} />
      <XAxis
        dataKey='created_at'
        tickLine={false}
        axisLine={false}
        minTickGap={35}
        tickFormatter={(value: number) =>
          dateFormatter.format(new Date(value * 1000))
        }
      />
      <YAxis
        tickLine={false}
        axisLine={false}
        width={58}
        tickFormatter={(value: number) => formatCompactNumber(value, locale)}
      />
      <ChartTooltip content={<UsageTooltipContent />} />
      <ChartLegend content={<ChartLegendContent />} />
    </>
  )

  return (
    <PanelWrapper
      title={title}
      description={description}
      empty={props.cacheOnly && !hasCache}
      emptyMessage={t(
        'No cache data was reported for this range. This does not mean a cache miss.'
      )}
      height='h-64'
    >
      <ChartContainer
        config={config}
        className='aspect-auto h-64 w-full'
        role='img'
        aria-label={title}
      >
        {props.cacheOnly ? (
          <LineChart data={props.series} accessibilityLayer>
            {axis}
            <Line
              type='monotone'
              dataKey='cache_read_tokens'
              stroke='var(--color-cache_read_tokens)'
              strokeWidth={2}
              dot={{ r: 2.5, fill: 'var(--color-cache_read_tokens)' }}
              connectNulls={false}
            />
            <Line
              type='monotone'
              dataKey='cache_write_tokens'
              stroke='var(--color-cache_write_tokens)'
              strokeWidth={2}
              dot={{ r: 2.5, fill: 'var(--color-cache_write_tokens)' }}
              connectNulls={false}
            />
          </LineChart>
        ) : (
          <AreaChart data={props.series} accessibilityLayer>
            {axis}
            <Area
              type='monotone'
              dataKey='input_tokens'
              stackId='usage'
              stroke='var(--color-input_tokens)'
              fill='var(--color-input_tokens)'
              fillOpacity={0.2}
            />
            <Area
              type='monotone'
              dataKey='output_tokens'
              stackId='usage'
              stroke='var(--color-output_tokens)'
              fill='var(--color-output_tokens)'
              fillOpacity={0.15}
            />
          </AreaChart>
        )}
      </ChartContainer>
    </PanelWrapper>
  )
}

export function UsageTooltipContent(
  props: ComponentProps<typeof ChartTooltipContent>
) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const dateFormatter = new Intl.DateTimeFormat(locale, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
  const labels: Record<string, string> = {
    input_tokens: t('Input tokens'),
    output_tokens: t('Output tokens'),
    cache_read_tokens: t('Recorded cache read'),
    cache_write_tokens: t('Recorded cache write'),
  }
  return (
    <ChartTooltipContent
      {...props}
      labelFormatter={(_value, payload) => {
        const timestamp = Number(payload?.[0]?.payload?.created_at)
        return Number.isFinite(timestamp)
          ? dateFormatter.format(new Date(timestamp * 1000))
          : ''
      }}
      formatter={(value, name, item) => (
        <>
          <span
            className='size-2.5 shrink-0 rounded-sm'
            style={{ backgroundColor: item.color }}
            aria-hidden='true'
          />
          <span className='text-muted-foreground flex-1'>
            {labels[String(item.dataKey)] ?? String(name)}
          </span>
          <span className='text-foreground font-mono font-medium tabular-nums'>
            {formatNumber(Number(value), locale)}
          </span>
        </>
      )}
    />
  )
}
