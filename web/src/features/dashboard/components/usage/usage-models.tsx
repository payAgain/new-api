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
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Progress } from '@/components/ui/progress'
import { toIntlLocale } from '@/i18n/languages'
import { formatCompactNumber, formatNumber } from '@/lib/format'

import { getModelTokenShare } from '../../lib/usage'
import type { ModelUsage } from '../../usage-types'
import { PanelWrapper } from '../ui/panel-wrapper'

interface UsageModelsProps {
  models: ModelUsage[]
  totalTokens: number
  compact?: boolean
}

export function UsageModels(props: UsageModelsProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const unknownDisplay = t('Not reported')
  const columns: StaticDataTableColumn<ModelUsage>[] = [
    {
      id: 'model',
      header: t('Model'),
      cell: (model) => (
        <span
          className='inline-block max-w-64 truncate font-mono text-xs'
          title={model.model_name}
        >
          {model.model_name}
        </span>
      ),
    },
    {
      id: 'input',
      header: t('Input tokens'),
      className: 'text-right',
      cellClassName: 'text-right',
      cell: (model) => formatNumber(model.input_tokens, locale),
    },
    {
      id: 'output',
      header: t('Output tokens'),
      className: 'text-right',
      cellClassName: 'text-right',
      cell: (model) => formatNumber(model.output_tokens, locale),
    },
    {
      id: 'read',
      header: t('Recorded cache read'),
      className: 'text-right',
      cellClassName: 'text-right',
      cell: (model) =>
        model.cache_read_tokens == null
          ? unknownDisplay
          : formatNumber(model.cache_read_tokens, locale),
    },
    {
      id: 'write',
      header: t('Recorded cache write'),
      className: 'text-right',
      cellClassName: 'text-right',
      cell: (model) =>
        model.cache_write_tokens == null
          ? unknownDisplay
          : formatNumber(model.cache_write_tokens, locale),
    },
    {
      id: 'ratio',
      header: t('Recorded cache share'),
      className: 'text-right',
      cellClassName: 'text-right',
      cell: (model) =>
        model.cache_ratio == null
          ? unknownDisplay
          : `${formatNumber(model.cache_ratio * 100, locale)}%`,
    },
    {
      id: 'total',
      header: t('Total tokens'),
      className: 'text-right',
      cellClassName: 'text-right font-mono font-semibold tabular-nums',
      cell: (model) => formatNumber(model.total_tokens, locale),
    },
    {
      id: 'requests',
      header: t('Requests'),
      className: 'text-right text-muted-foreground',
      cellClassName: 'text-right text-muted-foreground',
      cell: (model) => formatNumber(model.request_count, locale),
    },
  ]

  if (props.compact) {
    return (
      <PanelWrapper
        title={t('Top models by tokens')}
        description={t('Ranked by logged input + output tokens')}
      >
        <ol className='flex flex-col gap-4'>
          {props.models.slice(0, 5).map((model) => {
            const share = getModelTokenShare(model, props.totalTokens)
            return (
              <li
                key={model.model_name}
                className='flex min-w-0 flex-col gap-2'
              >
                <div className='flex min-w-0 items-start justify-between gap-3'>
                  <span
                    className='min-w-0 truncate font-mono text-xs'
                    title={model.model_name}
                  >
                    {model.model_name}
                  </span>
                  <span className='shrink-0 text-right font-mono text-xs tabular-nums'>
                    {formatCompactNumber(model.total_tokens, locale)}
                    <span className='text-muted-foreground ml-2'>
                      {formatNumber(share * 100, locale)}%
                    </span>
                  </span>
                </div>
                <Progress value={share * 100} aria-label={model.model_name} />
              </li>
            )
          })}
        </ol>
      </PanelWrapper>
    )
  }

  return (
    <PanelWrapper
      title={t('Usage by model')}
      description={t(
        'Sorted by total tokens. Cache reads and writes are included in input; missing cache values remain unknown.'
      )}
      contentClassName='p-0 sm:p-0'
    >
      <StaticDataTable
        data={props.models}
        columns={columns}
        getRowKey={(model) => model.model_name}
        tableProps={{ 'aria-label': t('Usage by model') }}
        tableClassName='min-w-[900px]'
        emptyContent={t('No model usage in this time range')}
      />
    </PanelWrapper>
  )
}
