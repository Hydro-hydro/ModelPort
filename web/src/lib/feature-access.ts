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
  | 'system_tasks'
  | 'media_tasks'
  | 'performance_console'
  | 'task_plugins'
  | 'deployments'
  | 'multi_node'

export type FeatureAccessData = {
  usage_mode?: string
  features?: Partial<Record<FeatureName, boolean>>
}

const OPTIONAL_FEATURES = new Set<FeatureName>([
  'system_tasks',
  'media_tasks',
  'task_plugins',
  'deployments',
  'multi_node',
])

export const TASK_LOG_FEATURES = ['media_tasks', 'task_plugins'] as const

export function featureAccessFromStatus(
  status:
    | {
        usage_mode?: unknown
        features?: unknown
      }
    | null
    | undefined
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
  if (!capabilities?.features) return !OPTIONAL_FEATURES.has(feature)
  return capabilities.features[feature] ?? !OPTIONAL_FEATURES.has(feature)
}

export function isAnyFeatureEnabled(
  capabilities: FeatureAccessData | null | undefined,
  features: readonly FeatureName[]
): boolean {
  return features.some((feature) => isFeatureEnabled(capabilities, feature))
}

export function isTaskLogsEnabled(
  capabilities: FeatureAccessData | null | undefined
): boolean {
  return isAnyFeatureEnabled(capabilities, TASK_LOG_FEATURES)
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
    // Optional modules fail closed when the capability endpoint is unavailable.
    return { enabled: !OPTIONAL_FEATURES.has(feature) }
  }
}

export async function getFreshAnyFeatureAccess(
  features: readonly FeatureName[]
): Promise<{ enabled: boolean; usageMode?: string }> {
  try {
    const status = await getStatus()
    const capabilities = featureAccessFromStatus(status)
    return {
      enabled: isAnyFeatureEnabled(capabilities, features),
      usageMode: capabilities.usage_mode,
    }
  } catch {
    return {
      enabled: features.some((feature) => !OPTIONAL_FEATURES.has(feature)),
    }
  }
}

export function useFeatureAccess() {
  const { status } = useStatus()
  const capabilities = useMemo<FeatureAccessData>(
    () => featureAccessFromStatus(status),
    [status]
  )

  return {
    capabilities,
    isEnabled: (feature: FeatureName) =>
      isFeatureEnabled(capabilities, feature),
    isTaskLogsEnabled: () => isTaskLogsEnabled(capabilities),
  }
}
