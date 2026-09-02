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
import { Loader2, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { PasswordInput } from '@/components/password-input'
import { Button } from '@/components/ui/button'

import type { SecureVerificationState, VerificationMethod } from '../types'

interface SecureVerificationDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  state: SecureVerificationState
  onVerify: (method: VerificationMethod, code?: string) => void | Promise<void>
  onCancel: () => void
  onCodeChange: (code: string) => void
}

export function SecureVerificationDialog({
  open,
  onOpenChange,
  state,
  onVerify,
  onCancel,
  onCodeChange,
}: SecureVerificationDialogProps) {
  const { t } = useTranslation()
  const title = state.title ?? t('Additional verification required')
  const description =
    state.description ??
    t('Confirm your identity before accessing this sensitive action.')
  const handleVerify = () => {
    onVerify('password', state.code)
  }
  const verifyDisabled = state.loading || !state.code.trim()

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={
        <>
          <ShieldCheck className='text-primary h-5 w-5' />
          {title}
        </>
      }
      description={description}
      contentClassName='top-[8vh] max-w-[calc(100%-1.5rem)] translate-y-0 overflow-hidden border-none shadow-xl sm:top-1/2 sm:max-w-md sm:translate-y-[-50%] sm:rounded-xl'
      headerClassName='border-b pb-4 text-left'
      titleClassName='flex items-center gap-2 text-lg font-semibold'
      descriptionClassName='text-left'
      contentHeight='auto'
      bodyClassName='px-1 py-1'
      showCloseButton={!state.loading}
      footerClassName='bg-muted/30 border-t px-6 py-4 sm:flex-row sm:justify-end'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            disabled={state.loading}
            onClick={onCancel}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            onClick={handleVerify}
            disabled={verifyDisabled}
          >
            {state.loading && <Loader2 className='h-4 w-4 animate-spin' />}
            {t('Verify')}
          </Button>
        </>
      }
    >
      <div className='space-y-3'>
        <p className='text-sm font-medium'>{t('Password')}</p>
        <PasswordInput
          value={state.code}
          onChange={(event) => onCodeChange(event.target.value)}
          placeholder={t('Enter password')}
          autoComplete='current-password'
          disabled={state.loading}
          autoFocus
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !verifyDisabled) {
              event.preventDefault()
              handleVerify()
            }
          }}
        />
      </div>
    </Dialog>
  )
}
