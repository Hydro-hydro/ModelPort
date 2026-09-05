import { describe, expect, test } from 'vitest'

import { isAnyFeatureEnabled, isTaskLogsEnabled } from './feature-access'

describe('task log feature access', () => {
  test('allows task logs when media tasks are enabled', () => {
    expect(
      isTaskLogsEnabled({
        features: { media_tasks: true, task_plugins: false },
      })
    ).toBe(true)
  })

  test('allows task logs when task plugins are enabled', () => {
    expect(
      isTaskLogsEnabled({
        features: { media_tasks: false, task_plugins: true },
      })
    ).toBe(true)
  })

  test('hides task logs when both task-producing features are disabled', () => {
    expect(
      isTaskLogsEnabled({
        features: { media_tasks: false, task_plugins: false },
      })
    ).toBe(false)
  })

  test('does not treat an unavailable optional feature as enabled', () => {
    expect(
      isAnyFeatureEnabled(undefined, ['media_tasks', 'task_plugins'])
    ).toBe(false)
  })
})
