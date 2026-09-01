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
import { createFileRoute, redirect } from '@tanstack/react-router'

import { BillingSettings } from '@/features/system-settings/billing'

import {
  BILLING_DEFAULT_SECTION,
  BILLING_SECTION_IDS,
  getBillingSectionMeta,
  type BillingSectionId,
} from '@/features/system-settings/billing/section-registry.tsx'
import { getFreshFeatureAccess } from '@/lib/feature-access'

export const Route = createFileRoute(
  '/_authenticated/system-settings/billing/$section'
)({
  beforeLoad: async ({ params }) => {
    const validSections = BILLING_SECTION_IDS as unknown as string[]
    if (!validSections.includes(params.section)) {
      throw redirect({
        to: '/system-settings/billing/$section',
        params: { section: BILLING_DEFAULT_SECTION },
      })
    }

    const feature = getBillingSectionMeta(params.section as BillingSectionId).feature
    if (feature) {
      const access = await getFreshFeatureAccess(feature)
      if (!access.enabled) {
        throw redirect({ to: '/system-settings/site' })
      }
    }
  },
  component: BillingSettings,
})
