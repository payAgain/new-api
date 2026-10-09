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
import { zodResolver } from '@hookform/resolvers/zod'
import { Filter } from 'lucide-react'
import { useId, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import dayjs from '@/lib/dayjs'

import type { UsageFilters as UsageFilterValues } from '../../usage-types'

interface UsageFiltersProps {
  filters: UsageFilterValues
  allAccounts: boolean
  onApply: (filters: UsageFilterValues) => void
  onReset: () => void
}

export function UsageFilters(props: UsageFiltersProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const formId = useId()
  const schema = z
    .object({
      start: z.date({ error: t('Choose a start time.') }),
      end: z.date({ error: t('Choose an end time.') }),
      granularity: z.enum(['hour', 'day']),
      model: z
        .string()
        .max(256, t('Model names must be at most 256 characters.')),
      username: z
        .string()
        .max(64, t('Usernames must be at most 64 characters.')),
    })
    .superRefine((values, context) => {
      const duration = values.end.getTime() - values.start.getTime()
      if (duration <= 0 || duration > 31 * 86400000) {
        context.addIssue({
          code: 'custom',
          path: ['end'],
          message: t(
            'Choose a time range of up to 31 days with the end after the start.'
          ),
        })
      }
    })
  const form = useForm<UsageFilterValues>({
    resolver: zodResolver(schema),
    defaultValues: props.filters,
  })

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) form.reset(props.filters)
    setOpen(nextOpen)
  }

  return (
    <Dialog
      open={open}
      onOpenChange={handleOpenChange}
      trigger={
        <Button variant='outline' size='sm'>
          <Filter data-icon='inline-start' />
          {t('Usage filters')}
        </Button>
      }
      title={t('Usage filters')}
      description={t(
        'Analyze up to 31 days of usage. Model and username filters use exact names.'
      )}
      footer={
        <>
          <Button
            variant='outline'
            onClick={() => {
              props.onReset()
              setOpen(false)
            }}
          >
            {t('Reset filters')}
          </Button>
          <Button type='submit' form={formId}>
            {t('Apply filters')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={formId}
          className='grid gap-4'
          onSubmit={form.handleSubmit((values) => {
            props.onApply(values)
            setOpen(false)
          })}
        >
          <div className='grid gap-4 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='start'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Start time')}</FormLabel>
                  <FormControl>
                    <Input
                      type='datetime-local'
                      value={
                        field.value
                          ? dayjs(field.value).format('YYYY-MM-DDTHH:mm')
                          : ''
                      }
                      onChange={(event) =>
                        field.onChange(
                          event.target.value
                            ? new Date(event.target.value)
                            : undefined
                        )
                      }
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='end'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('End time')}</FormLabel>
                  <FormControl>
                    <Input
                      type='datetime-local'
                      value={
                        field.value
                          ? dayjs(field.value).format('YYYY-MM-DDTHH:mm')
                          : ''
                      }
                      onChange={(event) =>
                        field.onChange(
                          event.target.value
                            ? new Date(event.target.value)
                            : undefined
                        )
                      }
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
          <FormField
            control={form.control}
            name='granularity'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Time granularity')}</FormLabel>
                <Select value={field.value} onValueChange={field.onChange}>
                  <FormControl>
                    <SelectTrigger className='w-full'>
                      <SelectValue>
                        {field.value === 'hour' ? t('Hour') : t('Day')}
                      </SelectValue>
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value='hour'>{t('Hour')}</SelectItem>
                      <SelectItem value='day'>{t('Day')}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='model'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Model name')}</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    maxLength={256}
                    placeholder={t('All models')}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          {props.allAccounts && (
            <FormField
              control={form.control}
              name='username'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Username')}</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      maxLength={64}
                      placeholder={t('All accounts')}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          )}
        </form>
      </Form>
    </Dialog>
  )
}
