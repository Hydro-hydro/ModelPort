/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormLabel,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'

type OptionalFeatureSettings = {
  'feature.system_tasks': boolean
  'feature.media_tasks': boolean
  'feature.task_plugins': boolean
  'feature.deployments': boolean
  'feature.multi_node': boolean
}

type OptionalFeaturesSectionProps = {
  defaultValues: OptionalFeatureSettings
}

const FEATURE_DEFINITIONS = [
  {
    key: 'feature.system_tasks',
    title: 'System Tasks',
    description: 'Run maintenance and background task workers.',
  },
  {
    key: 'feature.media_tasks',
    title: 'Media Tasks',
    description: 'Enable video and other asynchronous media task adapters.',
  },
  {
    key: 'feature.task_plugins',
    title: 'Task Plugins',
    description: 'Run task plugin adapters and the task API.',
  },
  {
    key: 'feature.deployments',
    title: 'Deployments',
    description: 'Enable external model deployment management.',
  },
  {
    key: 'feature.multi_node',
    title: 'Multi-node',
    description: 'Enable node reporting and multi-instance coordination.',
  },
] as const

export function OptionalFeaturesSection({
  defaultValues,
}: OptionalFeaturesSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const formDefaults = useMemo(() => ({ ...defaultValues }), [defaultValues])
  const form = useForm<OptionalFeatureSettings>({
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  const onSubmit = async (values: OptionalFeatureSettings) => {
    const updates = FEATURE_DEFINITIONS.filter(
      ({ key }) => values[key] !== defaultValues[key]
    )

    for (const { key } of updates) {
      await updateOption.mutateAsync({ key, value: String(values[key]) })
    }
    form.reset(values)
  }

  return (
    <SettingsSection title={t('Optional Features')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending || form.formState.isSubmitting}
            isSaveDisabled={!form.formState.isDirty}
            resetLabel='Reset'
            saveLabel='Save optional features'
          />
          <Alert>
            <AlertTitle>{t('Restart required')}</AlertTitle>
            <AlertDescription>
              {t(
                'Changes to optional capabilities take effect after the next restart. Saved options override environment defaults on subsequent starts.'
              )}
            </AlertDescription>
          </Alert>
          {FEATURE_DEFINITIONS.map(({ key, title, description }) => (
            <FormField
              key={key}
              control={form.control}
              name={key}
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t(title)}</FormLabel>
                    <FormDescription>{t(description)}</FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={Boolean(field.value)}
                      onCheckedChange={field.onChange}
                      disabled={updateOption.isPending || form.formState.isSubmitting}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
          ))}
          <p className='text-muted-foreground text-xs lg:col-span-2'>
            {t(
              'Task Plugins and Media Tasks automatically require System Tasks for polling and maintenance.'
            )}
          </p>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
