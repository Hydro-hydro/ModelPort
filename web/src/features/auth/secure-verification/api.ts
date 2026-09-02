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
import i18next from 'i18next'

import { encryptPassword } from '@/features/auth/lib/password-encryption'
import type { ApiResponse } from '@/features/auth/types'
import { api } from '@/lib/api'

import type {
  SecurityProof,
  SecurityProofScope,
  VerificationMethod,
  VerificationMethods,
} from './types'

const availableVerificationMethods: VerificationMethods = {
  password: true,
}

/**
 * Password confirmation is always available for the single administrator.
 */
export async function checkVerificationMethods(): Promise<VerificationMethods> {
  return availableVerificationMethods
}

/**
 * Submit an encrypted administrator password and receive a short-lived proof.
 */
export async function verify(
  method: VerificationMethod,
  scope: SecurityProofScope,
  password?: string
): Promise<SecurityProof> {
  if (method !== 'password') {
    throw new Error(
      i18next.t('Unsupported verification method: {{method}}', { method })
    )
  }

  const trimmed = password?.trim()
  if (!trimmed) {
    throw new Error(i18next.t('Please enter your password'))
  }

  const encryptedPassword = await encryptPassword(trimmed)
  const res = await api.post<ApiResponse<SecurityProof>>('/api/verify', {
    method: 'password',
    password_encrypted: encryptedPassword.password_encrypted,
    encryption_key_id: encryptedPassword.encryption_key_id,
    scope,
  })

  if (!res.data?.success) {
    throw new Error(res.data?.message || i18next.t('Verification failed'))
  }
  if (!res.data.data?.proof_token) {
    throw new Error(i18next.t('Verification proof was not returned'))
  }
  return res.data.data
}
