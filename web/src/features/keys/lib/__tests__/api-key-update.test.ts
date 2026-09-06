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
*/
import { describe, expect, test } from 'vitest'

import {
  buildApiKeyUpdatePayload,
  getApiKeyFormDefaultValues,
  transformFormDataToPayload,
} from '../api-key-form'

const expectedToken = { remain_quota: 120, used_quota: 30 }

describe('API key update payload', () => {
  test('omits quota fields for metadata-only edits', () => {
    const basePayload = transformFormDataToPayload({
      ...getApiKeyFormDefaultValues(false),
      name: 'renamed key',
    })

    const payload = buildApiKeyUpdatePayload(basePayload, {}, expectedToken)

    expect(payload.name).toBe('renamed key')
    expect(payload).not.toHaveProperty('remain_quota')
    expect(payload).not.toHaveProperty('unlimited_quota')
    expect(payload).not.toHaveProperty('expected_remain_quota')
    expect(payload).not.toHaveProperty('expected_used_quota')
  })

  test('includes edited quota and the accounting snapshot', () => {
    const basePayload = transformFormDataToPayload({
      ...getApiKeyFormDefaultValues(false),
      name: 'quota key',
      remain_quota_dollars: 5,
      unlimited_quota: false,
    })

    const payload = buildApiKeyUpdatePayload(
      basePayload,
      { remain_quota_dollars: true },
      expectedToken
    )

    expect(payload.remain_quota).toBeGreaterThan(0)
    expect(payload.unlimited_quota).toBe(false)
    expect(payload.expected_remain_quota).toBe(120)
    expect(payload.expected_used_quota).toBe(30)
  })
})
