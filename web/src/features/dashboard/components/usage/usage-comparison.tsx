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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'
import { Progress } from '@/components/ui/progress'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatCompactNumber, formatNumber } from '@/lib/format'
import { useSystemConfigStore } from '@/stores/system-config-store'

import type { ModelUsage, UsageMetrics } from '../../usage-types'
import { PanelWrapper } from '../ui/panel-wrapper'
import { UsageModels } from './usage-models'

type RankingMetric = 'quota' | 'total_tokens' | 'request_count' | 'cache_ratio'
const METRIC_LABELS = {
  quota: 'API quota used',
  total_tokens: 'Total tokens',
  request_count: 'Requests',
  cache_ratio: 'Recorded cache share',
} as const

export function UsageComparison(props: {
  models: ModelUsage[]
  summary: UsageMetrics
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  useSystemConfigStore((state) => state.config.currency)
  const [metric, setMetric] = useState<RankingMetric>('quota')
  const [search, setSearch] = useState('')
  const models = useMemo(
    () =>
      props.models
        .filter((model) =>
          model.model_name.toLowerCase().includes(search.trim().toLowerCase())
        )
        .sort(
          (a, b) =>
            (b[metric] ?? -1) - (a[metric] ?? -1) ||
            a.model_name.localeCompare(b.model_name)
        ),
    [props.models, metric, search]
  )
  const maximum = Math.max(...models.map((model) => model[metric] ?? 0), 0)
  const formatValue = (value: number | null) => {
    if (value === null) return t('Not reported')
    if (metric === 'quota') {
      return formatQuotaWithCurrency(value, { locale, abbreviate: false })
    }
    if (metric === 'cache_ratio') return `${formatNumber(value * 100, locale)}%`
    return formatCompactNumber(value, locale)
  }
  const leaders = [
    {
      title: 'Most quota used',
      model: [...props.models].sort((a, b) => b.quota - a.quota)[0],
      kind: 'quota',
    },
    {
      title: 'Most tokens used',
      model: [...props.models].sort(
        (a, b) => b.total_tokens - a.total_tokens
      )[0],
      kind: 'tokens',
    },
    {
      title: 'Highest recorded cache share',
      model: props.models
        .filter((model) => model.cache_ratio !== null)
        .sort((a, b) => (b.cache_ratio ?? 0) - (a.cache_ratio ?? 0))[0],
      kind: 'cache',
    },
  ]
  return (
    <div className='flex min-w-0 flex-col gap-4'>
      <div className='grid gap-3 md:grid-cols-3'>
        {leaders.map((leader) => {
          let value = t('Not reported')
          if (leader.model) {
            if (leader.kind === 'quota') {
              value = formatQuotaWithCurrency(leader.model.quota, {
                locale,
                abbreviate: false,
              })
            }
            if (leader.kind === 'tokens') {
              value = formatCompactNumber(leader.model.total_tokens, locale)
            }
            if (leader.kind === 'cache') {
              value = `${formatNumber((leader.model.cache_ratio ?? 0) * 100, locale)}%`
            }
          }
          return (
            <section
              key={leader.title}
              aria-label={t(leader.title)}
              className='bg-card flex min-w-0 flex-col gap-2 rounded-xl border p-4'
            >
              <h3 className='text-muted-foreground text-xs'>
                {t(leader.title)}
              </h3>
              <strong className='font-mono text-xl tabular-nums'>
                {value}
              </strong>
              <span
                className='truncate font-mono text-xs'
                title={leader.model?.model_name}
              >
                {leader.model?.model_name ?? t('Not reported')}
              </span>
            </section>
          )
        })}
      </div>
      <PanelWrapper
        title={t('Model rankings')}
        description={t(
          'Compare actual quota, token volume, requests and recorded cache share.'
        )}
      >
        <div className='mb-5 flex flex-wrap items-center justify-between gap-3'>
          <Tabs
            value={metric}
            onValueChange={(value) => {
              if (value in METRIC_LABELS) setMetric(value as RankingMetric)
            }}
          >
            <TabsList
              aria-label={t('Rank models by')}
              className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'
            >
              {Object.entries(METRIC_LABELS).map(([key, label]) => (
                <TabsTrigger key={key} value={key}>
                  {t(label)}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          <Input
            maxLength={256}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('Search models')}
            aria-label={t('Search models')}
            className='w-full sm:w-64'
          />
        </div>
        <ol className='grid gap-4 lg:grid-cols-2'>
          {models.map((model, index) => (
            <li
              key={model.model_name}
              className='flex min-w-0 flex-col gap-2 rounded-lg border p-3'
            >
              <div className='flex items-start justify-between gap-3'>
                <span
                  className='min-w-0 truncate font-mono text-xs'
                  title={model.model_name}
                >
                  <span className='text-muted-foreground mr-2'>
                    {index + 1}.
                  </span>
                  {model.model_name}
                </span>
                <strong className='shrink-0 font-mono text-sm tabular-nums'>
                  {formatValue(model[metric])}
                </strong>
              </div>
              {model[metric] !== null && (
                <Progress
                  aria-label={model.model_name}
                  value={maximum ? ((model[metric] ?? 0) / maximum) * 100 : 0}
                />
              )}
              <div className='text-muted-foreground flex flex-wrap justify-between gap-2 text-xs'>
                <span>
                  {t('Requests')}: {formatNumber(model.request_count, locale)}
                </span>
                <span>
                  {t('Input tokens')}:{' '}
                  {formatCompactNumber(model.input_tokens, locale)} /{' '}
                  {t('Output tokens')}:{' '}
                  {formatCompactNumber(model.output_tokens, locale)}
                </span>
              </div>
            </li>
          ))}
        </ol>
        {models.length === 0 && (
          <p className='text-muted-foreground py-6 text-center text-sm'>
            {t('No matching models')}
          </p>
        )}
      </PanelWrapper>
      <UsageModels
        models={models}
        totalTokens={props.summary.total_tokens}
        totalQuota={props.summary.quota}
      />
    </div>
  )
}
