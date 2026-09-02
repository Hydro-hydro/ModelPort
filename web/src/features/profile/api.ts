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
import { api } from '@/lib/api'
import type { LoginSession } from '@/stores/auth-store'

import type {
  ApiResponse,
  UserProfile,
  UpdateUserRequest,
  UpdateUserSettingsRequest,
} from './types'

// ============================================================================
// User Profile APIs
// ============================================================================

/**
 * Get current user profile
 */
export async function getUserProfile(): Promise<ApiResponse<UserProfile>> {
  const res = await api.get('/api/user/self')
  return res.data
}

/**
 * Update user profile
 */
export async function updateUserProfile(
  data: UpdateUserRequest
): Promise<ApiResponse> {
  const res = await api.put('/api/user/self', data, {
    acceptAuthRotation: Boolean(data.password),
  })
  return res.data
}

/**
 * Update user settings
 */
export async function updateUserSettings(
  data: UpdateUserSettingsRequest
): Promise<ApiResponse> {
  const res = await api.put('/api/user/setting', data)
  return res.data
}

/**
 * Update interface language preference
 */
export async function updateUserLanguage(
  language: string
): Promise<ApiResponse> {
  const res = await api.put('/api/user/self', { language })
  return res.data
}

/**
 * Generate/regenerate system access token
 */
export async function generateAccessToken(): Promise<ApiResponse<string>> {
  const res = await api.get('/api/user/token')
  return res.data
}

// ============================================================================
// Login Session APIs
// ============================================================================

export async function getLoginSessions(): Promise<ApiResponse<LoginSession[]>> {
  const res = await api.get('/api/user/sessions')
  return res.data
}

export async function revokeLoginSession(sid: string): Promise<ApiResponse> {
  const res = await api.delete(`/api/user/sessions/${encodeURIComponent(sid)}`)
  return res.data
}

export async function revokeOtherLoginSessions(): Promise<ApiResponse> {
  const res = await api.post('/api/user/sessions/revoke-others')
  return res.data
}
