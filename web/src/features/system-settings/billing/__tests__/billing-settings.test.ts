/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or (at your
option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { TFunction } from 'i18next'
import { Receipt } from 'lucide-react'
import { describe, expect, test } from 'vitest'

import { SYSTEM_SETTINGS_VIEW } from '@/components/layout/config/system-settings.config'
import {
  BILLING_DEFAULT_SECTION,
  BILLING_SECTION_IDS,
  getBillingSectionNavItems,
} from '@/features/system-settings/billing/section-registry'
import { DEFAULT_SYSTEM_NAME } from '@/lib/constants'
import { parseCurrencyDisplayType } from '@/lib/currency'
import { DEFAULT_CURRENCY_CONFIG } from '@/stores/system-config-store'

const translate = ((key: string) => key) as TFunction

describe('personal billing settings', () => {
  test('removes quota settings from billing navigation and opens currency by default', () => {
    expect(BILLING_SECTION_IDS).toEqual([
      'currency',
      'model-pricing',
      'group-pricing',
    ])
    expect(BILLING_DEFAULT_SECTION).toBe('currency')
    expect(getBillingSectionNavItems(translate)).toEqual([
      {
        title: 'Currency & Display',
        url: '/system-settings/billing/currency',
      },
      {
        title: 'Model Pricing',
        url: '/system-settings/billing/model-pricing',
      },
      {
        title: 'Group Configuration',
        url: '/system-settings/billing/group-pricing',
      },
    ])
  })

  test('uses CNY as the default currency display type', () => {
    expect(DEFAULT_CURRENCY_CONFIG.quotaDisplayType).toBe('CNY')
    expect(parseCurrencyDisplayType(undefined)).toBe('CNY')
  })

  test('uses the ModelPort default system name', () => {
    expect(DEFAULT_SYSTEM_NAME).toBe('ModelPort')
  })

  test('shows the billing icon in system administration navigation', () => {
    const groups = SYSTEM_SETTINGS_VIEW.getNavGroups(translate)
    const billingItem = groups
      .flatMap((group) => group.items)
      .find((item) => item.title === 'Pricing & Usage')

    expect(billingItem).toBeDefined()
    expect(billingItem?.icon).toBe(Receipt)
  })
})
