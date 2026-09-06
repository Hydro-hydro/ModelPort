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
import { Link } from '@tanstack/react-router'
import { ArrowRight, BookOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { Footer } from '@/components/layout/components/footer'
import { Button } from '@/components/ui/button'
import { useStatus } from '@/hooks/use-status'
import { useSystemConfig } from '@/hooks/use-system-config'

export function Home() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const { systemName, logo } = useSystemConfig()
  const docsUrl =
    (status?.docs_link as string | undefined) || 'https://docs.newapi.pro'
  const displayName = systemName || 'ModelPort'

  return (
    <PublicLayout showMainContainer={false}>
      <main className='flex min-h-[calc(100svh-4rem)] items-center px-6 py-16'>
        <section className='mx-auto w-full max-w-4xl'>
          <div className='max-w-2xl'>
            <div className='mb-6 flex items-center gap-3'>
              <img
                src={logo || '/logo.png'}
                alt={displayName}
                className='size-10 rounded-xl object-contain'
              />
              <span className='text-muted-foreground text-sm font-medium'>
                {t('Self-hosted AI API gateway')}
              </span>
            </div>
            <h1 className='text-4xl leading-tight font-semibold tracking-tight sm:text-5xl'>
              {displayName}
            </h1>
            <p className='text-muted-foreground mt-5 max-w-xl text-base leading-relaxed sm:text-lg'>
              {t(
                'Connect your applications to configured AI channels through one local gateway.'
              )}
            </p>
            <div className='mt-8 flex flex-wrap items-center gap-3'>
              <Button
                className='h-10 rounded-lg px-4'
                render={<Link to='/sign-in' />}
              >
                {t('Sign in')}
                <ArrowRight data-icon='inline-end' />
              </Button>
              <Button
                variant='outline'
                className='h-10 rounded-lg px-4'
                render={
                  <a href={docsUrl} target='_blank' rel='noopener noreferrer' />
                }
              >
                <BookOpen data-icon='inline-start' />
                {t('Docs')}
              </Button>
            </div>
          </div>
        </section>
      </main>
      <Footer />
    </PublicLayout>
  )
}
