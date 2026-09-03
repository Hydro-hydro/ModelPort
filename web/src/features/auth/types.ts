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
import type { FeatureName } from '@/lib/feature-access'
import type { AuthBundle } from '@/stores/auth-store'

export interface LoginPayload {
  username?: string
  password: string
  turnstile?: string
  passwordEncryptionEnabled?: boolean
}

export interface LoginResponse {
  success: boolean
  message: string
  data?: AuthBundle
}

export interface ApiResponse<T = unknown> {
  success: boolean
  message: string
  data?: T
}

interface SystemStatusData {
  version?: string
  system_name?: string
  logo?: string
  turnstile_check?: boolean
  turnstile_site_key?: string
  display_in_currency?: boolean
  display_token_stat_enabled?: boolean
  quota_per_unit?: number
  quota_display_type?: string
  usd_exchange_rate?: number
  custom_currency_symbol?: string
  custom_currency_exchange_rate?: number
  password_login_enabled?: boolean
  password_login_encryption_enabled?: boolean
  usage_mode?: string
  features?: Partial<Record<FeatureName, boolean>>
  [key: string]: unknown
}

export interface SystemStatus extends SystemStatusData {
  success?: boolean
  message?: string
  data?: SystemStatusData
}

export interface AuthFormProps extends React.HTMLAttributes<HTMLFormElement> {
  redirectTo?: string
}
