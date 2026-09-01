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

import { ModelSettings } from '@/features/system-settings/models'

import {
  MODELS_DEFAULT_SECTION,
  MODELS_SECTION_IDS,
  getModelsSectionMeta,
  type ModelSectionId,
} from '@/features/system-settings/models/section-registry.tsx'
import { getFreshFeatureAccess } from '@/lib/feature-access'

export const Route = createFileRoute(
  '/_authenticated/system-settings/models/$section'
)({
  beforeLoad: async ({ params }) => {
    const validSections = MODELS_SECTION_IDS as unknown as string[]
    if (!validSections.includes(params.section)) {
      throw redirect({
        to: '/system-settings/models/$section',
        params: { section: MODELS_DEFAULT_SECTION },
      })
    }

    const feature = getModelsSectionMeta(params.section as ModelSectionId).feature
    if (feature) {
      const access = await getFreshFeatureAccess(feature)
      if (!access.enabled) {
        throw redirect({ to: '/system-settings/site' })
      }
    }
  },
  component: ModelSettings,
})
