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
import { Upload } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { MarketplacePanel } from './components/marketplace-panel'
import { PluginDetailSheet } from './components/plugin-detail-sheet'
import { PluginsTable } from './components/plugins-table'
import { UploadDialog } from './components/upload-dialog'
import type { TaskPluginListItem } from './types'

export function TaskPlugins() {
  const { t } = useTranslation()
  const [detail, setDetail] = useState<TaskPluginListItem | null>(null)
  const [tab, setTab] = useState('installed')
  const [uploadKey, setUploadKey] = useState<string | null>(null)
  const [uploadOpen, setUploadOpen] = useState(false)
  const openUpload = (key?: string) => {
    setUploadKey(key ?? null)
    setUploadOpen(true)
  }
  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>{t('Task Plugins')}</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          {tab === 'installed' && (
            <Button onClick={() => openUpload()}>
              <Upload />
              {t('Upload plugin')}
            </Button>
          )}
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <Tabs
            value={tab}
            onValueChange={setTab}
            className='flex h-full min-h-0 flex-col gap-3'
          >
            <TabsList className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'>
              <TabsTrigger value='installed'>{t('Installed')}</TabsTrigger>
              <TabsTrigger value='marketplace'>{t('Marketplace')}</TabsTrigger>
            </TabsList>
            <TabsContent value='installed' className='min-h-0 flex-1'>
              <PluginsTable
                onDetails={setDetail}
                onUpload={(key) => openUpload(key)}
              />
            </TabsContent>
            <TabsContent value='marketplace' className='min-h-0 flex-1'>
              <MarketplacePanel />
            </TabsContent>
          </Tabs>
        </SectionPageLayout.Content>
      </SectionPageLayout>
      <PluginDetailSheet
        key={detail?.meta.key ?? ''}
        plugin={detail}
        onOpenChange={(open) => {
          if (!open) setDetail(null)
        }}
      />
      <UploadDialog
        open={uploadOpen}
        onOpenChange={setUploadOpen}
        initialKey={uploadKey ?? undefined}
      />
    </>
  )
}
