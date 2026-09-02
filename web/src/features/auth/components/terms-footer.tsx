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

import { cn } from '@/lib/utils'

import type { SystemStatus } from '../types'

interface TermsFooterProps {
  className?: string
  status?: SystemStatus | null
}

export function TermsFooter({ className, status }: TermsFooterProps) {
  const { t } = useTranslation()
  const hasUserAgreement = Boolean(status?.user_agreement_enabled)
  const hasPrivacyPolicy = Boolean(status?.privacy_policy_enabled)

  if (!hasUserAgreement && !hasPrivacyPolicy) return null

  const activeLinks = [
    hasUserAgreement
      ? { label: t('User Agreement'), href: '/user-agreement' }
      : null,
    hasPrivacyPolicy
      ? { label: t('Privacy Policy'), href: '/privacy-policy' }
      : null,
  ].filter(Boolean) as Array<{ label: string; href: string }>
  const [firstLink, secondLink] = activeLinks

  return (
    <p className={cn('text-muted-foreground text-center text-xs', className)}>
      {t('By clicking sign in, you agree to our')}{' '}
      {firstLink && (
        <a
          href={firstLink.href}
          className='hover:text-primary underline underline-offset-4'
        >
          {firstLink.label}
        </a>
      )}
      {secondLink && (
        <>
          {' '}
          {t('and')}{' '}
          <a
            href={secondLink.href}
            className='hover:text-primary underline underline-offset-4'
          >
            {secondLink.label}
          </a>
        </>
      )}
      .
    </p>
  )
}
