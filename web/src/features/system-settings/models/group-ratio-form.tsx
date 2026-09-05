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
import { Code2, Eye } from 'lucide-react'
import { memo, useCallback, useState, type BaseSyntheticEvent } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageActionsPortal } from '../components/settings-page-context'
import { safeNumberFieldProps } from '../utils/numeric-field'
import { RouteGroupEditor } from './route-group-editor'

export type GroupFormValues = {
  GroupRatio: string
  GroupGroupRatio: string
  AutoGroups: string
  MaxTokenAutoGroups: number
  DefaultUseAutoGroup: boolean
}

type GroupRatioFormProps = {
  form: UseFormReturn<GroupFormValues>
  onSave: (values: GroupFormValues) => Promise<void>
  isSaving: boolean
}

export const GroupRatioForm = memo(function GroupRatioForm({
  form,
  onSave,
  isSaving,
}: GroupRatioFormProps) {
  const { t } = useTranslation()
  const [editMode, setEditMode] = useState<'visual' | 'json'>('visual')
  const [visualEditorValid, setVisualEditorValid] = useState(true)
  const submit = useCallback(
    (event?: BaseSyntheticEvent) => {
      event?.preventDefault()
      if (editMode === 'visual' && !visualEditorValid) return
      void form.handleSubmit(onSave)(event)
    },
    [editMode, form, onSave, visualEditorValid]
  )

  const handleFieldChange = useCallback(
    (field: 'GroupRatio' | 'GroupGroupRatio' | 'AutoGroups', value: string) => {
      form.setValue(field, value, {
        shouldValidate: true,
        shouldDirty: true,
      })
    },
    [form]
  )

  return (
    <Form {...form}>
      <SettingsPageActionsPortal>
        <Button
          type='button'
          size='sm'
          onClick={submit}
          disabled={isSaving || (editMode === 'visual' && !visualEditorValid)}
        >
          {isSaving ? t('Saving...') : t('Save group ratios')}
        </Button>
      </SettingsPageActionsPortal>

      <div className='flex justify-end'>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() =>
            setEditMode((mode) => (mode === 'visual' ? 'json' : 'visual'))
          }
        >
          {editMode === 'visual' ? (
            <>
              <Code2 className='mr-2 h-4 w-4' aria-hidden='true' />
              {t('Switch to JSON')}
            </>
          ) : (
            <>
              <Eye className='mr-2 h-4 w-4' aria-hidden='true' />
              {t('Switch to Visual')}
            </>
          )}
        </Button>
      </div>

      {editMode === 'visual' ? (
        <div className='space-y-6'>
          <RouteGroupEditor
            groupRatio={form.watch('GroupRatio')}
            groupGroupRatio={form.watch('GroupGroupRatio')}
            autoGroups={form.watch('AutoGroups')}
            onChange={handleFieldChange}
            onValidityChange={setVisualEditorValid}
          />

          <SettingsForm onSubmit={submit}>
            <FormField
              control={form.control}
              name='MaxTokenAutoGroups'
              render={({ field, fieldState }) => (
                <FormItem data-invalid={fieldState.invalid}>
                  <FormLabel>{t('Maximum custom groups per token')}</FormLabel>
                  <FormControl>
                    <Input
                      {...safeNumberFieldProps(field)}
                      type='number'
                      min={1}
                      step={1}
                      aria-invalid={fieldState.invalid}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Limits only token-specific Auto snapshots. Global Auto inheritance remains unlimited.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='DefaultUseAutoGroup'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Default to auto groups')}</FormLabel>
                    <FormDescription>
                      {t(
                        'When enabled, newly created tokens start in the first auto group.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
          </SettingsForm>
        </div>
      ) : (
        <SettingsForm onSubmit={submit}>
          <FormField
            control={form.control}
            name='GroupRatio'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Group ratios')}</FormLabel>
                <FormControl>
                  <JsonCodeEditor
                    value={field.value}
                    onChange={field.onChange}
                    name={field.name}
                    onBlur={field.onBlur}
                    textareaRef={field.ref}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='GroupGroupRatio'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Inter-group overrides')}</FormLabel>
                <FormControl>
                  <JsonCodeEditor
                    value={field.value}
                    onChange={field.onChange}
                    name={field.name}
                    onBlur={field.onBlur}
                    textareaRef={field.ref}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Only configured combinations are overridden. All other calls keep the route group base ratio.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='AutoGroups'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Auto assignment order')}</FormLabel>
                <FormControl>
                  <JsonCodeEditor
                    value={field.value}
                    onChange={field.onChange}
                    name={field.name}
                    onBlur={field.onBlur}
                    textareaRef={field.ref}
                    heightClassName='h-40 min-h-40 max-h-40'
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'JSON array of group identifiers. When enabled below, new tokens rotate through this list.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='MaxTokenAutoGroups'
            render={({ field, fieldState }) => (
              <FormItem data-invalid={fieldState.invalid}>
                <FormLabel>{t('Maximum custom groups per token')}</FormLabel>
                <FormControl>
                  <Input
                    {...safeNumberFieldProps(field)}
                    type='number'
                    min={1}
                    step={1}
                    aria-invalid={fieldState.invalid}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Limits only token-specific Auto snapshots. Global Auto inheritance remains unlimited.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='DefaultUseAutoGroup'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Default to auto groups')}</FormLabel>
                  <FormDescription>
                    {t(
                      'When enabled, newly created tokens start in the first auto group.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
        </SettingsForm>
      )}
    </Form>
  )
})
