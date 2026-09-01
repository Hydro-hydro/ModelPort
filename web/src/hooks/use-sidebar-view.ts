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
import { useLocation } from '@tanstack/react-router'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { resolveSidebarView } from '@/components/layout/lib/sidebar-view-registry'
import type { NavGroup, NavItem, ResolvedSidebarView } from '@/components/layout/types'
import { type FeatureName, useFeatureAccess } from '@/lib/feature-access'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { useSidebarConfig } from './use-sidebar-config'
import { useSidebarData } from './use-sidebar-data'

/** Sentinel key used for the root navigation in animation `key=` props */
const ROOT_VIEW_KEY = '__root'

function filterFeatureNavItems(
  items: NavItem[],
  isEnabled: (feature: FeatureName) => boolean
): NavItem[] {
  return items.reduce<NavItem[]>((visibleItems, item) => {
    if (item.feature && !isEnabled(item.feature)) {
      return visibleItems
    }

    if ('items' in item && item.items) {
      const nestedItems = item.items.filter(
        (nestedItem) =>
          !nestedItem.feature || isEnabled(nestedItem.feature)
      )
      if (nestedItems.length === 0) {
        return visibleItems
      }
      visibleItems.push({ ...item, items: nestedItems })
      return visibleItems
    }

    visibleItems.push(item)
    return visibleItems
  }, [])
}

function filterFeatureNavGroups(
  navGroups: NavGroup[],
  isEnabled: (feature: FeatureName) => boolean
): NavGroup[] {
  return navGroups
    .map((group) => ({
      ...group,
      items: filterFeatureNavItems(group.items, isEnabled),
    }))
    .filter((group) => group.items.length > 0)
}

/**
 * Resolve the active sidebar view for the current location.
 *
 * - Returns the matching nested {@link SidebarView} (with its nav
 *   groups) when the URL belongs to a registered drill-in workspace.
 * - Otherwise returns the root navigation, narrowed by:
 *     · admin-only group visibility (role-based);
 *     · `useSidebarConfig` (admin × user `sidebar_modules` overlay).
 *
 * Nested views are intentionally NOT passed through `useSidebarConfig`
 * — those filters target known dashboard URLs only, and gating is
 * already enforced at the route level (`beforeLoad` redirects).
 */
export function useSidebarView(): ResolvedSidebarView {
  const { t } = useTranslation()
  const pathname = useLocation({ select: (l) => l.pathname })
  const userRole = useAuthStore((s) => s.auth.user?.role)
  const { isEnabled } = useFeatureAccess()
  const rootSidebarData = useSidebarData()
  const configFilteredRoot = useSidebarConfig(rootSidebarData.navGroups)

  const rootNavGroups = useMemo<NavGroup[]>(() => {
    const role = userRole ?? ROLE.GUEST
    const isAdmin = role >= ROLE.ADMIN
    const roleFilteredGroups = configFilteredRoot
      .filter((group) => (group.id === 'admin' ? isAdmin : true))
      .map((group) => {
        const items = group.items.filter(
          (item) => item.requiredRole === undefined || role >= item.requiredRole
        )
        return items.length === group.items.length ? group : { ...group, items }
      })

    return filterFeatureNavGroups(roleFilteredGroups, isEnabled)
  }, [configFilteredRoot, isEnabled, userRole])

  const view = resolveSidebarView(pathname)

  if (view) {
    return {
      key: view.id,
      view,
      navGroups: filterFeatureNavGroups(view.getNavGroups(t), isEnabled),
    }
  }

  return {
    key: ROOT_VIEW_KEY,
    view: null,
    navGroups: rootNavGroups,
  }
}
