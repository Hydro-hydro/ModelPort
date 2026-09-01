import { useMemo } from 'react'

import { useStatus } from '@/hooks/use-status'
import { getStatus } from '@/lib/api'

export type FeatureName =
  | 'core_relay'
  | 'channel_management'
  | 'model_management'
  | 'token_management'
  | 'request_logs'
  | 'protocol_diagnostics'
  | 'basic_auth'
  | 'advanced_auth'
  | 'media_tasks'
  | 'performance_console'
  | 'registration'
  | 'email_verification'
  | 'password_reset'
  | 'oauth'
  | 'user_management'
  | 'affiliation'
  | 'wallet'
  | 'payments'
  | 'subscriptions'
  | 'redemptions'
  | 'checkin'
  | 'pricing_portal'
  | 'rankings'
  | 'task_plugins'
  | 'deployments'
  | 'multi_node'

export type FeatureAccessData = {
  usage_mode?: string
  features?: Partial<Record<FeatureName, boolean>>
}

export function featureAccessFromStatus(
  status: {
    usage_mode?: unknown
    features?: unknown
  } | null | undefined
): FeatureAccessData {
  return {
    usage_mode:
      typeof status?.usage_mode === 'string' ? status.usage_mode : undefined,
    features: status?.features as
      | Partial<Record<FeatureName, boolean>>
      | undefined,
  }
}

export function isFeatureEnabled(
  capabilities: FeatureAccessData | null | undefined,
  feature: FeatureName
): boolean {
  if (!capabilities?.features) return true
  return capabilities.features[feature] !== false
}

export async function getFreshFeatureAccess(
  feature: FeatureName
): Promise<{ enabled: boolean; usageMode?: string }> {
  try {
    const status = await getStatus()
    const capabilities = featureAccessFromStatus(status)
    return {
      enabled: isFeatureEnabled(capabilities, feature),
      usageMode: capabilities.usage_mode,
    }
  } catch {
    // 状态接口异常时保持旧行为，避免前端误锁定功能。
    return { enabled: true }
  }
}

export function useFeatureAccess() {
  const { status } = useStatus()
  const capabilities = useMemo<FeatureAccessData>(
    () => featureAccessFromStatus(status),
    [status?.features, status?.usage_mode]
  )

  return {
    capabilities,
    isEnabled: (feature: FeatureName) =>
      isFeatureEnabled(capabilities, feature),
  }
}
