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
import { describe, expect, test } from 'vitest'

import {
  buildRouteGroupPricingRows,
  parseRouteAutoGroupList,
  parseRouteGroupOverrideMap,
  parseRouteGroupRatioMap,
  routeGroupPricingSignature,
  serializeRouteAutoGroupList,
  serializeRouteGroupOverrideMap,
  serializeRouteGroupPricingRows,
} from '../route-group-editor-utils'

describe('route group editor data adapters', () => {
  test('parses finite numeric ratios and ignores unknown values', () => {
    const result = parseRouteGroupRatioMap(
      '{"default":1,"premium":"2.5","nullValue":null,"nested":{},"invalid":"nope","items":[]}'
    )

    expect(result).toEqual({ default: 1, premium: 2.5 })
  })

  test('falls back to an empty ratio map for malformed or non-object JSON', () => {
    expect(parseRouteGroupRatioMap('{"default":')).toEqual({})
    expect(parseRouteGroupRatioMap('["default"]')).toEqual({})
  })

  test('serializes pricing rows with trimmed names and normalized ratios', () => {
    const serialized = serializeRouteGroupPricingRows([
      { _id: 'row-1', name: ' default ', ratio: '1.5' },
      { _id: 'row-2', name: ' ', ratio: '7' },
      { _id: 'row-3', name: 'premium', ratio: 'invalid' },
      { _id: 'row-4', name: 'default', ratio: '2' },
    ])

    expect(JSON.parse(serialized)).toEqual({ default: 2, premium: 1 })
  })

  test('builds pricing rows in source order with caller-provided ids', () => {
    const ids = ['row-1', 'row-2']
    const rows = buildRouteGroupPricingRows(
      '{"default":1,"premium":2}',
      () => ids.shift() ?? 'row-fallback'
    )

    expect(rows).toEqual([
      { _id: 'row-1', name: 'default', ratio: '1' },
      { _id: 'row-2', name: 'premium', ratio: '2' },
    ])
  })

  test('uses the normalized pricing map for the pricing signature', () => {
    const rows = [
      { _id: 'row-1', name: ' default ', ratio: '1' },
      { _id: 'row-2', name: 'premium', ratio: '2' },
    ]

    expect(routeGroupPricingSignature(rows)).toBe('{"default":1,"premium":2}')
  })

  test('parses nested overrides while dropping malformed entries', () => {
    const result = parseRouteGroupOverrideMap(
      '{"default":{"premium":"0.8","invalid":"nope","nullValue":null},"empty":{},"invalid":[],"premium":{"default":1}}'
    )

    expect(result).toEqual({
      default: { premium: 0.8 },
      premium: { default: 1 },
    })
  })

  test('serializes override maps as persisted nested JSON', () => {
    const serialized = serializeRouteGroupOverrideMap({
      default: { premium: 0.8 },
      premium: { default: 1 },
    })

    expect(JSON.parse(serialized)).toEqual({
      default: { premium: 0.8 },
      premium: { default: 1 },
    })
  })

  test('deduplicates and trims automatic groups while preserving first-seen order', () => {
    const result = parseRouteAutoGroupList(
      '[" premium ","premium",null,42,"default","default ",""]'
    )

    expect(result).toEqual(['premium', 'default'])
  })

  test('round-trips a reordered automatic assignment list', () => {
    const current = parseRouteAutoGroupList('["default","premium","fast"]')
    const reordered = [current[1], current[0], current[2]]

    expect(
      parseRouteAutoGroupList(serializeRouteAutoGroupList(reordered))
    ).toEqual(['premium', 'default', 'fast'])
  })

  test('falls back to an empty automatic group list for unknown JSON shapes', () => {
    expect(parseRouteAutoGroupList('{"default":1}')).toEqual([])
    expect(parseRouteAutoGroupList('not-json')).toEqual([])
  })
})
