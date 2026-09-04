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
import { safeJsonParse } from '../utils/json-parser'

export type RouteGroupPricingRow = {
  _id: string
  name: string
  ratio: string
}

export type RouteGroupOverrideMap = Record<string, Record<string, number>>

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function parseObject(value: string): Record<string, unknown> {
  const parsed = safeJsonParse<unknown>(value, {
    fallback: {},
    silent: true,
  })
  return isRecord(parsed) ? parsed : {}
}

function isFiniteNonNegativeNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0
}

export function isRouteGroupRatioJson(value: string): boolean {
  if (!value.trim()) return true
  const parsed = safeJsonParse<unknown>(value, {
    fallback: null,
    silent: true,
  })
  return (
    isRecord(parsed) &&
    Object.entries(parsed).every(
      ([name, ratio]) =>
        name.trim() !== '' &&
        name !== 'auto' &&
        isFiniteNonNegativeNumber(ratio)
    )
  )
}

export function parseRouteGroupRatioMap(value: string): Record<string, number> {
  const map = Object.create(null) as Record<string, number>
  for (const [name, ratio] of Object.entries(parseObject(value))) {
    if (typeof ratio !== 'number' && typeof ratio !== 'string') continue
    const parsed = Number(ratio)
    if (Number.isFinite(parsed)) map[name] = parsed
  }
  return map
}

export function serializeRouteGroupPricingRows(
  rows: RouteGroupPricingRow[]
): string {
  const map = Object.create(null) as Record<string, number>
  for (const row of rows) {
    const name = row.name.trim()
    if (!name) continue
    const ratio = Number(row.ratio)
    if (!Number.isFinite(ratio) || ratio < 0) continue
    map[name] = ratio
  }
  return JSON.stringify(map, null, 2)
}

export function buildRouteGroupPricingRows(
  value: string,
  createRowId: () => string
): RouteGroupPricingRow[] {
  return Object.entries(parseRouteGroupRatioMap(value)).map(
    ([name, ratio]) => ({
      _id: createRowId(),
      name,
      ratio: String(ratio),
    })
  )
}

export function routeGroupPricingSignature(
  rows: RouteGroupPricingRow[]
): string {
  return JSON.stringify(
    parseRouteGroupRatioMap(serializeRouteGroupPricingRows(rows))
  )
}

export function parseRouteGroupOverrideMap(
  value: string
): RouteGroupOverrideMap {
  const map = Object.create(null) as RouteGroupOverrideMap
  for (const [sourceGroup, rawOverrides] of Object.entries(
    parseObject(value)
  )) {
    if (!isRecord(rawOverrides)) continue
    const overrides = Object.create(null) as Record<string, number>
    for (const [targetGroup, ratio] of Object.entries(rawOverrides)) {
      if (typeof ratio !== 'number' && typeof ratio !== 'string') continue
      const parsed = Number(ratio)
      if (Number.isFinite(parsed)) overrides[targetGroup] = parsed
    }
    map[sourceGroup] = overrides
  }
  return map
}

export function isRouteGroupOverrideJson(value: string): boolean {
  if (!value.trim()) return true
  const parsed = safeJsonParse<unknown>(value, {
    fallback: null,
    silent: true,
  })
  if (!isRecord(parsed)) return false
  return Object.values(parsed).every(
    (rawOverrides) =>
      isRecord(rawOverrides) &&
      Object.values(rawOverrides).every((ratio) =>
        isFiniteNonNegativeNumber(ratio)
      )
  )
}

export function serializeRouteGroupOverrideMap(
  map: RouteGroupOverrideMap
): string {
  return JSON.stringify(map, null, 2)
}

export function parseRouteAutoGroupList(value: string): string[] {
  const parsed = safeJsonParse<unknown>(value, {
    fallback: [],
    silent: true,
  })
  if (!Array.isArray(parsed)) return []

  const groups: string[] = []
  const seen = new Set<string>()
  for (const item of parsed) {
    if (typeof item !== 'string') continue
    const group = item.trim()
    if (!group || seen.has(group)) continue
    seen.add(group)
    groups.push(group)
  }
  return groups
}

export function isRouteAutoGroupJson(value: string): boolean {
  if (!value.trim()) return true
  const parsed = safeJsonParse<unknown>(value, {
    fallback: null,
    silent: true,
  })
  if (!Array.isArray(parsed)) return false

  const seen = new Set<string>()
  for (const group of parsed) {
    if (typeof group !== 'string') return false
    const normalized = group.trim()
    if (!normalized || seen.has(normalized)) return false
    seen.add(normalized)
  }
  return true
}

export function serializeRouteAutoGroupList(groups: string[]): string {
  return JSON.stringify(groups, null, 2)
}
