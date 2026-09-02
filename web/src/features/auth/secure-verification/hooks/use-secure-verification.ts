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
import { useCallback, useMemo, useState } from 'react'
import { toast } from 'sonner'

import {
  extractVerificationInfo,
  isVerificationRequiredError,
} from '@/lib/secure-verification'

import { checkVerificationMethods, verify } from '../api'
import type {
  SecureVerificationState,
  StartVerificationOptions,
  UseSecureVerificationOptions,
  VerificationMethod,
  VerificationMethods,
} from '../types'

type ApiCall = ((proofToken?: string) => Promise<unknown>) | null

interface InternalState extends SecureVerificationState {
  apiCall: ApiCall
}

const initialState: InternalState = {
  method: 'password',
  loading: false,
  code: '',
  title: undefined,
  description: undefined,
  apiCall: null,
}

export function useSecureVerification(
  options: UseSecureVerificationOptions = {}
) {
  const { onSuccess, onError, successMessage, autoReset = true } = options
  const methods = useMemo<VerificationMethods>(() => ({ password: true }), [])
  const [state, setState] = useState<InternalState>(initialState)
  const [open, setOpen] = useState(false)

  const reset = useCallback(() => {
    setState(initialState)
    setOpen(false)
  }, [])

  const startVerification = useCallback(
    async (
      apiCall: (proofToken?: string) => Promise<unknown>,
      config: StartVerificationOptions
    ) => {
      const { scope, title, description } = config
      await checkVerificationMethods()
      setState((prev) => ({
        ...prev,
        apiCall,
        method: 'password',
        scope,
        title,
        description,
        code: '',
      }))
      setOpen(true)
      return true
    },
    []
  )

  const executeVerification = useCallback(
    async (method?: VerificationMethod, code?: string) => {
      if (!state.apiCall) {
        toast.error(i18next.t('Verification is not configured properly'))
        return
      }

      const actualMethod = method ?? state.method
      if (actualMethod !== 'password') {
        toast.error(i18next.t('Select a verification method first'))
        return
      }

      setState((prev) => ({ ...prev, loading: true }))

      try {
        if (!state.scope) {
          throw new Error(i18next.t('Verification scope is missing'))
        }
        const proof = await verify(
          actualMethod,
          state.scope,
          code ?? state.code
        )
        const result = await state.apiCall(proof.proof_token)

        if (successMessage) {
          toast.success(successMessage)
        }

        onSuccess?.(result, actualMethod)

        if (autoReset) {
          reset()
        }

        return result
      } catch (error) {
        const message =
          error instanceof Error
            ? error.message
            : i18next.t('Verification failed')
        toast.error(message)
        onError?.(error)
        throw error
      } finally {
        setState((prev) => ({ ...prev, loading: false }))
      }
    },
    [state, successMessage, onSuccess, onError, autoReset, reset]
  )

  const setCode = useCallback((code: string) => {
    setState((prev) => ({ ...prev, code }))
  }, [])

  const switchMethod = useCallback((method: VerificationMethod) => {
    setState((prev) => ({ ...prev, method, code: '' }))
  }, [])

  const cancel = useCallback(() => {
    reset()
  }, [reset])

  const withVerification = useCallback(
    async (
      apiCall: (proofToken?: string) => Promise<unknown>,
      config: StartVerificationOptions
    ) => {
      try {
        return await apiCall()
      } catch (error) {
        if (isVerificationRequiredError(error)) {
          const info = extractVerificationInfo(error)
          toast.info(info.message)
          await startVerification(apiCall, config)
          return null
        }
        throw error
      }
    },
    [startVerification]
  )

  const canUseMethod = useCallback(
    (method: VerificationMethod) => method === 'password',
    []
  )

  const recommendedMethod = useMemo<VerificationMethod>(
    () => 'password',
    []
  )

  return {
    open,
    setOpen,
    methods,
    state,
    startVerification,
    executeVerification,
    cancel,
    reset,
    setCode,
    switchMethod,
    withVerification,
    fetchVerificationMethods: checkVerificationMethods,
    canUseMethod,
    recommendedMethod,
    hasAnyMethod: true,
    isLoading: state.loading,
    currentMethod: state.method,
    code: state.code,
  }
}
