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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'
import { useSystemConfigStore } from '@/stores/system-config-store'

import type {
  UsageGranularity,
  UsageMetrics,
  UsageSeriesPoint,
} from '../../usage-types'
import { UsageCharts } from './usage-charts'

type Trend = 'tokens' | 'cache' | 'quota' | 'requests'

export function ModelUsageDetail(props: {
  model: string
  summary: UsageMetrics
  series: UsageSeriesPoint[]
  granularity: UsageGranularity
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  useSystemConfigStore((state) => state.config.currency)
  const [trend, setTrend] = useState<Trend>('tokens')
  const count = props.summary.request_count
  return (
    <section
      className='flex min-w-0 flex-col gap-3'
      aria-label={t('Selected model analysis')}
    >
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div className='flex min-w-0 flex-col gap-1'>
          <h3 className='font-mono text-sm font-semibold break-all'>
            {props.model}
          </h3>
          <p className='text-muted-foreground text-xs'>
            {t('Charged calls')}: {formatNumber(count, locale)}
            {' · '}
            {t('Quota per request')}:{' '}
            {formatQuotaWithCurrency(count ? props.summary.quota / count : 0, {
              locale,
              abbreviate: false,
            })}
            {' · '}
            {t('Tokens per request')}:{' '}
            {formatNumber(
              count ? props.summary.total_tokens / count : 0,
              locale
            )}
          </p>
        </div>
        <Tabs
          value={trend}
          onValueChange={(value) => setTrend(value as Trend)}
          className='max-w-full'
        >
          <TabsList
            aria-label={t('Model trend metric')}
            className='max-w-full flex-wrap group-data-horizontal/tabs:h-auto'
          >
            <TabsTrigger value='tokens'>{t('Input / output')}</TabsTrigger>
            <TabsTrigger value='cache'>{t('Cache')}</TabsTrigger>
            <TabsTrigger value='quota'>{t('API quota used')}</TabsTrigger>
            <TabsTrigger value='requests'>{t('Charged calls')}</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>
      <UsageCharts
        series={props.series}
        granularity={props.granularity}
        metric={trend}
      />
    </section>
  )
}
