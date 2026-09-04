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
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  GripVertical,
  Plus,
  Trash2,
} from 'lucide-react'
import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import {
  buildRouteGroupPricingRows,
  parseRouteAutoGroupList,
  parseRouteGroupOverrideMap,
  parseRouteGroupRatioMap,
  isRouteAutoGroupJson,
  isRouteGroupOverrideJson,
  isRouteGroupRatioJson,
  routeGroupPricingSignature,
  serializeRouteAutoGroupList,
  serializeRouteGroupOverrideMap,
  serializeRouteGroupPricingRows,
  type RouteGroupOverrideMap,
  type RouteGroupPricingRow,
} from './route-group-editor-utils'

export type RouteGroupField = 'GroupRatio' | 'GroupGroupRatio' | 'AutoGroups'

export type RouteGroupEditorProps = {
  groupRatio: string
  groupGroupRatio: string
  autoGroups: string
  onChange: (field: RouteGroupField, value: string) => void
  onValidityChange?: (valid: boolean) => void
}

const sectionCardClassName =
  'relative shadow-sm ring-0 before:pointer-events-none before:absolute before:inset-0 before:rounded-xl before:border before:border-border/90'
const sectionHeaderClassName = 'border-b bg-muted/20'

let rowIdCounter = 0
function createRowId() {
  rowIdCounter += 1
  return `route_group_${rowIdCounter}`
}

function renameReferences(
  oldName: string,
  newName: string,
  groupGroupRatio: string,
  autoGroups: string[],
  onChange: RouteGroupEditorProps['onChange']
) {
  const map = parseRouteGroupOverrideMap(groupGroupRatio)
  const renamed = Object.create(null) as RouteGroupOverrideMap

  for (const [source, overrides] of Object.entries(map)) {
    const nextOverrides = Object.create(null) as Record<string, number>
    for (const [target, ratio] of Object.entries(overrides)) {
      nextOverrides[target === oldName ? newName : target] = ratio
    }
    renamed[source] = nextOverrides
  }

  onChange('GroupGroupRatio', serializeRouteGroupOverrideMap(renamed))
  onChange(
    'AutoGroups',
    serializeRouteAutoGroupList(
      autoGroups.map((group) => (group === oldName ? newName : group))
    )
  )
}

function removeReferences(
  groupNames: string[],
  groupGroupRatio: string,
  autoGroups: string[],
  onChange: RouteGroupEditorProps['onChange']
) {
  const map = parseRouteGroupOverrideMap(groupGroupRatio)
  for (const overrides of Object.values(map)) {
    for (const groupName of groupNames) delete overrides[groupName]
  }

  onChange('GroupGroupRatio', serializeRouteGroupOverrideMap(map))
  onChange(
    'AutoGroups',
    serializeRouteAutoGroupList(
      autoGroups.filter((group) => !groupNames.includes(group))
    )
  )
}

type GroupNameSelectProps = {
  options: string[]
  value: string
  placeholder: string
  onValueChange: (value: string) => void
  ariaLabel: string
  disabled?: boolean
}

function GroupNameSelect(props: GroupNameSelectProps) {
  return (
    <Select
      value={props.value || null}
      onValueChange={(value) => {
        if (typeof value === 'string' && value) props.onValueChange(value)
      }}
    >
      <SelectTrigger
        className='w-full'
        aria-label={props.ariaLabel}
        disabled={props.disabled}
      >
        <SelectValue placeholder={props.placeholder} />
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false}>
        <SelectGroup>
          {props.options.map((name) => (
            <SelectItem key={name} value={name}>
              {name}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}

function GroupReference({ name, known }: { name: string; known: boolean }) {
  const { t } = useTranslation()
  return (
    <span className='inline-flex items-center gap-1'>
      <span>{name}</span>
      {!known && (
        <AlertTriangle
          className='text-destructive h-3.5 w-3.5'
          aria-label={t('Not in pricing table')}
        />
      )}
    </span>
  )
}

type PricingTableProps = Pick<
  RouteGroupEditorProps,
  'groupRatio' | 'groupGroupRatio' | 'autoGroups' | 'onChange'
> & {
  onLocalValidityChange?: (valid: boolean) => void
}

function canPersistPricingRows(rows: RouteGroupPricingRow[]): boolean {
  const names = new Set<string>()
  for (const row of rows) {
    const name = row.name.trim()
    const ratioText = row.ratio.trim()
    const ratio = Number(ratioText)
    if (
      !name ||
      name === 'auto' ||
      names.has(name) ||
      !ratioText ||
      !Number.isFinite(ratio) ||
      ratio < 0
    ) {
      return false
    }
    names.add(name)
  }
  return true
}

function RouteGroupPricingTable(props: PricingTableProps) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<RouteGroupPricingRow[]>(() =>
    buildRouteGroupPricingRows(props.groupRatio, createRowId)
  )

  const {
    autoGroups,
    groupGroupRatio,
    groupRatio,
    onChange,
    onLocalValidityChange,
  } = props
  const groupRatioJsonValid = isRouteGroupRatioJson(groupRatio)
  const committedNamesRef = useRef<Map<string, string> | null>(null)
  if (committedNamesRef.current === null) {
    committedNamesRef.current = new Map(
      rows.map((row) => [row._id, row.name.trim()])
    )
  }

  useEffect(() => {
    if (!groupRatioJsonValid) return
    const sourceSignature = routeGroupPricingSignature(
      buildRouteGroupPricingRows(groupRatio, createRowId)
    )
    setRows((current) => {
      if (routeGroupPricingSignature(current) === sourceSignature) {
        return current
      }
      const nextRows = buildRouteGroupPricingRows(groupRatio, createRowId)
      committedNamesRef.current = new Map(
        nextRows.map((row) => [row._id, row.name.trim()])
      )
      return nextRows
    })
  }, [groupRatio, groupRatioJsonValid])

  const emitRows = useCallback(
    (nextRows: RouteGroupPricingRow[]) => {
      setRows(nextRows)
      const valid =
        canPersistPricingRows(nextRows) &&
        isRouteGroupRatioJson(groupRatio) &&
        isRouteGroupOverrideJson(groupGroupRatio) &&
        isRouteAutoGroupJson(autoGroups)
      onLocalValidityChange?.(valid)
      if (valid) {
        onChange('GroupRatio', serializeRouteGroupPricingRows(nextRows))
      }
    },
    [autoGroups, groupGroupRatio, groupRatio, onChange, onLocalValidityChange]
  )

  const currentRowsValid =
    canPersistPricingRows(rows) &&
    isRouteGroupRatioJson(groupRatio) &&
    isRouteGroupOverrideJson(groupGroupRatio) &&
    isRouteAutoGroupJson(autoGroups)

  useEffect(() => {
    onLocalValidityChange?.(currentRowsValid)
  }, [currentRowsValid, onLocalValidityChange])

  const updateRow = useCallback(
    (rowId: string, field: 'name' | 'ratio', value: string) => {
      const currentRow = rows.find((row) => row._id === rowId)
      if (!currentRow) return
      const nextRows = rows.map((row) =>
        row._id === rowId ? { ...row, [field]: value } : row
      )
      emitRows(nextRows)
    },
    [emitRows, rows]
  )

  const syncNameReferencesOnBlur = useCallback(
    (rowId: string) => {
      const row = rows.find((item) => item._id === rowId)
      if (!row) return
      if (!isRouteGroupRatioJson(groupRatio)) return
      const nextName = row.name.trim()
      const oldName = committedNamesRef.current?.get(rowId) ?? nextName
      if (
        !nextName ||
        nextName === 'auto' ||
        oldName === nextName ||
        !isRouteGroupOverrideJson(groupGroupRatio) ||
        !isRouteAutoGroupJson(autoGroups) ||
        rows.some((item) => item._id !== rowId && item.name.trim() === nextName)
      ) {
        return
      }
      renameReferences(
        oldName,
        nextName,
        groupGroupRatio,
        parseRouteAutoGroupList(autoGroups),
        onChange
      )
      committedNamesRef.current?.set(rowId, nextName)
    },
    [autoGroups, groupGroupRatio, groupRatio, onChange, rows]
  )

  const addRow = useCallback(() => {
    const existingNames = new Set(rows.map((row) => row.name.trim()))
    let index = 1
    let name = `group_${index}`
    while (existingNames.has(name)) {
      index += 1
      name = `group_${index}`
    }
    const nextRow = { _id: createRowId(), name: name.trim(), ratio: '1' }
    emitRows([...rows, nextRow])
    committedNamesRef.current?.set(nextRow._id, nextRow.name)
  }, [emitRows, rows])

  const removeRow = useCallback(
    (rowId: string) => {
      const row = rows.find((item) => item._id === rowId)
      if (!row) return
      if (!isRouteGroupRatioJson(groupRatio)) return
      emitRows(rows.filter((item) => item._id !== rowId))
      const previousName = committedNamesRef.current?.get(rowId)
      committedNamesRef.current?.delete(rowId)
      const names = [
        ...new Set(
          [previousName, row.name.trim()].filter((name): name is string =>
            Boolean(name)
          )
        ),
      ]
      if (
        names.length > 0 &&
        isRouteGroupOverrideJson(groupGroupRatio) &&
        isRouteAutoGroupJson(autoGroups)
      ) {
        removeReferences(
          names,
          groupGroupRatio,
          parseRouteAutoGroupList(autoGroups),
          onChange
        )
      }
    },
    [autoGroups, emitRows, groupGroupRatio, groupRatio, onChange, rows]
  )

  const duplicateNames = useMemo(() => {
    const counts = new Map<string, number>()
    for (const row of rows) {
      const name = row.name.trim()
      if (name) counts.set(name, (counts.get(name) ?? 0) + 1)
    }
    return [...counts.entries()]
      .filter(([, count]) => count > 1)
      .map(([name]) => name)
  }, [rows])

  return (
    <Card className={sectionCardClassName}>
      <CardHeader className={sectionHeaderClassName}>
        <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
          <div>
            <CardTitle>{t('Pricing groups')}</CardTitle>
            <CardDescription>{t('Group ratios')}</CardDescription>
          </div>
          <Button
            type='button'
            onClick={addRow}
            size='sm'
            disabled={!groupRatioJsonValid}
          >
            <Plus className='mr-2 h-4 w-4' />
            {t('Add group')}
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {!groupRatioJsonValid && (
          <p className='text-destructive mb-3 text-sm' role='alert'>
            {t('Invalid JSON format or values out of allowed range')}
          </p>
        )}
        <StaticDataTable
          data={rows}
          getRowKey={(row) => row._id}
          emptyClassName='text-muted-foreground h-20 text-sm'
          emptyContent={t('No groups yet. Add a group to get started.')}
          columns={[
            {
              id: 'group',
              header: t('Group name'),
              className: 'min-w-40',
              cell: (row) => (
                <Input
                  value={row.name}
                  aria-label={t('Group name')}
                  onChange={(event) =>
                    updateRow(row._id, 'name', event.target.value)
                  }
                  onBlur={() => syncNameReferencesOnBlur(row._id)}
                  aria-invalid={
                    !row.name.trim() ||
                    row.name.trim() === 'auto' ||
                    duplicateNames.includes(row.name.trim())
                  }
                />
              ),
            },
            {
              id: 'ratio',
              header: t('Ratio'),
              className: 'w-32',
              cell: (row) => (
                <Input
                  type='number'
                  min={0}
                  step={0.1}
                  value={row.ratio}
                  aria-label={t('Ratio')}
                  aria-invalid={
                    row.ratio.trim() === '' ||
                    !Number.isFinite(Number(row.ratio)) ||
                    Number(row.ratio) < 0
                  }
                  onChange={(event) =>
                    updateRow(row._id, 'ratio', event.target.value)
                  }
                />
              ),
            },
            {
              id: 'actions',
              header: t('Actions'),
              className: 'w-20 text-right',
              cellClassName: 'text-right',
              cell: (row) => (
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-sm'
                  aria-label={t('Remove {{group}}', { group: row.name })}
                  onClick={() => removeRow(row._id)}
                >
                  <Trash2 />
                </Button>
              ),
            },
          ]}
        />
        {duplicateNames.length > 0 && (
          <p className='text-destructive mt-3 text-sm'>
            {t('Duplicate group names: {{names}}', {
              names: duplicateNames.join(', '),
            })}
          </p>
        )}
      </CardContent>
    </Card>
  )
}

type OverrideRow = {
  sourceGroup: string
  targetGroup: string
  ratio: number
}

type OverrideDraft = {
  sourceGroup: string
  targetGroup: string
  ratio: string
}

type OverrideTableProps = Pick<
  RouteGroupEditorProps,
  'groupRatio' | 'groupGroupRatio' | 'onChange'
>

function RouteGroupOverrideTable(props: OverrideTableProps) {
  const { t } = useTranslation()
  const { groupGroupRatio, groupRatio, onChange } = props
  const groupGroupRatioJsonValid = isRouteGroupOverrideJson(groupGroupRatio)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editData, setEditData] = useState<OverrideRow | null>(null)
  const [draft, setDraft] = useState<OverrideDraft>({
    sourceGroup: '',
    targetGroup: '',
    ratio: '',
  })

  const overrideMap = useMemo(
    () => parseRouteGroupOverrideMap(groupGroupRatio),
    [groupGroupRatio]
  )
  const configuredGroupNames = useMemo(
    () =>
      new Set(
        Object.keys(parseRouteGroupRatioMap(groupRatio)).filter(
          (group) => group !== 'auto'
        )
      ),
    [groupRatio]
  )
  const groupNames = useMemo(() => {
    const names = new Set(configuredGroupNames)
    for (const [source, overrides] of Object.entries(overrideMap)) {
      names.add(source)
      for (const target of Object.keys(overrides)) names.add(target)
    }
    return [...names]
  }, [configuredGroupNames, overrideMap])
  const rows = useMemo<OverrideRow[]>(
    () =>
      Object.entries(overrideMap).flatMap(([sourceGroup, overrides]) =>
        Object.entries(overrides).map(([targetGroup, ratio]) => ({
          sourceGroup,
          targetGroup,
          ratio,
        }))
      ),
    [overrideMap]
  )

  const openAdd = useCallback(() => {
    setEditData(null)
    setDraft({
      sourceGroup: groupNames[0] ?? '',
      targetGroup: groupNames[0] ?? '',
      ratio: '',
    })
    setDialogOpen(true)
  }, [groupNames])

  const openEdit = useCallback((row: OverrideRow) => {
    setEditData(row)
    setDraft({
      sourceGroup: row.sourceGroup,
      targetGroup: row.targetGroup,
      ratio: String(row.ratio),
    })
    setDialogOpen(true)
  }, [])

  const closeDialog = useCallback((open: boolean) => {
    setDialogOpen(open)
    if (!open) {
      setEditData(null)
      setDraft({ sourceGroup: '', targetGroup: '', ratio: '' })
    }
  }, [])

  const saveOverride = useCallback(() => {
    const sourceGroup = draft.sourceGroup.trim()
    const targetGroup = draft.targetGroup.trim()
    const ratio = Number(draft.ratio)
    if (!sourceGroup || !targetGroup || !Number.isFinite(ratio) || ratio < 0) {
      return
    }

    if (!groupGroupRatioJsonValid) return
    const nextMap = parseRouteGroupOverrideMap(groupGroupRatio)
    const existingOverride = nextMap[sourceGroup]
      ? Object.hasOwn(nextMap[sourceGroup], targetGroup)
      : false
    const isCurrentOverride =
      editData?.sourceGroup === sourceGroup &&
      editData.targetGroup === targetGroup
    if (existingOverride && !isCurrentOverride) return
    if (editData) {
      if (
        editData.sourceGroup !== sourceGroup ||
        editData.targetGroup !== targetGroup
      ) {
        delete nextMap[editData.sourceGroup]?.[editData.targetGroup]
        if (nextMap[editData.sourceGroup]) {
          if (Object.keys(nextMap[editData.sourceGroup]).length === 0) {
            delete nextMap[editData.sourceGroup]
          }
        }
      }
    }
    nextMap[sourceGroup] ??= Object.create(null) as Record<string, number>
    nextMap[sourceGroup][targetGroup] = ratio
    onChange('GroupGroupRatio', serializeRouteGroupOverrideMap(nextMap))
    closeDialog(false)
  }, [
    closeDialog,
    draft,
    editData,
    groupGroupRatio,
    groupGroupRatioJsonValid,
    onChange,
  ])

  const deleteOverride = useCallback(
    (row: OverrideRow) => {
      if (!groupGroupRatioJsonValid) return
      const nextMap = parseRouteGroupOverrideMap(groupGroupRatio)
      delete nextMap[row.sourceGroup]?.[row.targetGroup]
      if (
        nextMap[row.sourceGroup] &&
        Object.keys(nextMap[row.sourceGroup]).length === 0
      ) {
        delete nextMap[row.sourceGroup]
      }
      onChange('GroupGroupRatio', serializeRouteGroupOverrideMap(nextMap))
    },
    [groupGroupRatio, groupGroupRatioJsonValid, onChange]
  )

  const canSave =
    draft.sourceGroup.trim() !== '' &&
    draft.targetGroup.trim() !== '' &&
    draft.ratio.trim() !== '' &&
    Number.isFinite(Number(draft.ratio)) &&
    Number(draft.ratio) >= 0 &&
    !rows.some(
      (row) =>
        row.sourceGroup === draft.sourceGroup.trim() &&
        row.targetGroup === draft.targetGroup.trim() &&
        (editData === null ||
          row.sourceGroup !== editData.sourceGroup ||
          row.targetGroup !== editData.targetGroup)
    )

  return (
    <Card className={sectionCardClassName}>
      <CardHeader className={sectionHeaderClassName}>
        <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
          <div>
            <CardTitle>{t('Inter-group ratio overrides')}</CardTitle>
            <CardDescription>
              {t(
                'Only configured combinations are overridden. All other calls keep the billing group base ratio.'
              )}
            </CardDescription>
          </div>
          <Button
            type='button'
            onClick={openAdd}
            size='sm'
            disabled={groupNames.length === 0 || !groupGroupRatioJsonValid}
          >
            <Plus className='mr-2 h-4 w-4' />
            {t('Add ratio override')}
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {!groupGroupRatioJsonValid && (
          <p className='text-destructive mb-3 text-sm' role='alert'>
            {t('Invalid JSON format or values out of allowed range')}
          </p>
        )}
        <StaticDataTable
          data={rows}
          getRowKey={(row) =>
            JSON.stringify([row.sourceGroup, row.targetGroup])
          }
          emptyClassName='text-muted-foreground h-20 text-sm'
          emptyContent={t('No groups yet. Add a group to get started.')}
          columns={[
            {
              id: 'source',
              header: t('Route group'),
              cellClassName: 'font-medium',
              cell: (row) => (
                <GroupReference
                  name={row.sourceGroup}
                  known={configuredGroupNames.has(row.sourceGroup)}
                />
              ),
            },
            {
              id: 'target',
              header: t('Target group'),
              cell: (row) => (
                <GroupReference
                  name={row.targetGroup}
                  known={configuredGroupNames.has(row.targetGroup)}
                />
              ),
            },
            {
              id: 'ratio',
              header: t('Ratio'),
              className: 'w-28 text-right',
              cellClassName: 'text-right font-mono',
              cell: (row) => row.ratio,
            },
            {
              id: 'actions',
              header: t('Actions'),
              className: 'w-28 text-right',
              cellClassName: 'text-right',
              cell: (row) => (
                <StaticRowActions
                  editLabel={t('Edit')}
                  deleteLabel={t('Delete')}
                  menuLabel={t('Open menu')}
                  onEdit={() => openEdit(row)}
                  onDelete={() => deleteOverride(row)}
                />
              ),
            },
          ]}
        />

        <Dialog
          open={dialogOpen}
          onOpenChange={closeDialog}
          title={editData ? t('Edit ratio override') : t('Add ratio override')}
          description={t('Route group')}
          contentClassName='sm:max-w-[500px]'
          contentHeight='auto'
          bodyClassName='space-y-4'
          footer={
            <>
              <Button
                type='button'
                variant='outline'
                onClick={() => closeDialog(false)}
              >
                {t('Cancel')}
              </Button>
              <Button type='button' onClick={saveOverride} disabled={!canSave}>
                {editData ? t('Update') : t('Add')}
              </Button>
            </>
          }
        >
          <div className='space-y-4 py-1'>
            <div className='space-y-2'>
              <Label>{t('Route group')}</Label>
              <GroupNameSelect
                options={groupNames}
                value={draft.sourceGroup}
                placeholder={t('Select a group')}
                ariaLabel={t('Route group')}
                onValueChange={(sourceGroup) =>
                  setDraft((current) => ({ ...current, sourceGroup }))
                }
              />
            </div>
            <div className='space-y-2'>
              <Label>{t('Target group')}</Label>
              <GroupNameSelect
                options={groupNames}
                value={draft.targetGroup}
                placeholder={t('Select a group')}
                ariaLabel={t('Target group')}
                onValueChange={(targetGroup) =>
                  setDraft((current) => ({ ...current, targetGroup }))
                }
              />
            </div>
            <div className='space-y-2'>
              <Label>{t('Ratio')}</Label>
              <Input
                type='number'
                min={0}
                step={0.1}
                value={draft.ratio}
                aria-label={t('Ratio')}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    ratio: event.target.value,
                  }))
                }
              />
            </div>
          </div>
        </Dialog>
      </CardContent>
    </Card>
  )
}

type AutoGroupEditorProps = Pick<
  RouteGroupEditorProps,
  'groupRatio' | 'autoGroups' | 'onChange'
>

function RouteGroupAutoEditor(props: AutoGroupEditorProps) {
  const { t } = useTranslation()
  const { autoGroups, groupRatio, onChange } = props
  const groups = useMemo(
    () => parseRouteAutoGroupList(autoGroups),
    [autoGroups]
  )
  const routeGroups = useMemo(
    () =>
      Object.keys(parseRouteGroupRatioMap(groupRatio)).filter(
        (group) => group !== 'auto'
      ),
    [groupRatio]
  )
  const autoGroupsJsonValid = isRouteAutoGroupJson(autoGroups)
  const candidates = useMemo(
    () => routeGroups.filter((group) => !groups.includes(group)),
    [groups, routeGroups]
  )

  const emitGroups = useCallback(
    (nextGroups: string[]) => {
      if (!autoGroupsJsonValid) return
      onChange('AutoGroups', serializeRouteAutoGroupList(nextGroups))
    },
    [autoGroupsJsonValid, onChange]
  )

  const moveGroup = useCallback(
    (index: number, direction: 'up' | 'down') => {
      const targetIndex = direction === 'up' ? index - 1 : index + 1
      if (targetIndex < 0 || targetIndex >= groups.length) return
      const nextGroups = [...groups]
      ;[nextGroups[index], nextGroups[targetIndex]] = [
        nextGroups[targetIndex],
        nextGroups[index],
      ]
      emitGroups(nextGroups)
    },
    [emitGroups, groups]
  )

  return (
    <Card className={sectionCardClassName}>
      <CardHeader className={sectionHeaderClassName}>
        <CardTitle>{t('Auto assignment order')}</CardTitle>
        <CardDescription>
          {t(
            'Priority order for tokens in the auto group. The system tries groups from top to bottom.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent>
        {!autoGroupsJsonValid && (
          <p className='text-destructive mb-3 text-sm' role='alert'>
            {t('Invalid JSON format or values out of allowed range')}
          </p>
        )}
        <div className='space-y-3'>
          <GroupNameSelect
            options={candidates}
            value=''
            placeholder={t('Add group')}
            ariaLabel={t('Add group')}
            disabled={candidates.length === 0 || !autoGroupsJsonValid}
            onValueChange={(group) => emitGroups([...groups, group])}
          />
          {groups.length === 0 ? (
            <p className='text-muted-foreground text-sm'>
              {t('No groups yet. Add a group to get started.')}
            </p>
          ) : (
            <div className='space-y-2'>
              {groups.map((group, index) => (
                <div
                  key={group}
                  className='flex items-center gap-2 rounded-md border p-2'
                >
                  <GripVertical
                    className='text-muted-foreground h-4 w-4 shrink-0'
                    aria-hidden='true'
                  />
                  <span className='min-w-0 flex-1 truncate font-medium'>
                    {group}
                  </span>
                  {!routeGroups.includes(group) && (
                    <span className='text-destructive inline-flex items-center gap-1 text-xs'>
                      <AlertTriangle
                        className='h-3.5 w-3.5'
                        aria-hidden='true'
                      />
                      {t('Not in pricing table')}
                    </span>
                  )}
                  <div className='flex shrink-0 gap-1'>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon-sm'
                      disabled={index === 0}
                      aria-label={t('Move {{group}} up', { group })}
                      onClick={() => moveGroup(index, 'up')}
                    >
                      <ArrowUp />
                    </Button>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon-sm'
                      disabled={index === groups.length - 1}
                      aria-label={t('Move {{group}} down', { group })}
                      onClick={() => moveGroup(index, 'down')}
                    >
                      <ArrowDown />
                    </Button>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon-sm'
                      aria-label={t('Remove {{group}}', { group })}
                      onClick={() =>
                        emitGroups(
                          groups.filter(
                            (_group: string, itemIndex: number) =>
                              itemIndex !== index
                          )
                        )
                      }
                    >
                      <Trash2 />
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

export const RouteGroupEditor = memo(function RouteGroupEditor(
  props: RouteGroupEditorProps
) {
  const onValidityChange = props.onValidityChange
  const [localRowsValid, setLocalRowsValid] = useState(true)
  const parsedValuesValid =
    isRouteGroupRatioJson(props.groupRatio) &&
    isRouteGroupOverrideJson(props.groupGroupRatio) &&
    isRouteAutoGroupJson(props.autoGroups)
  const valid = parsedValuesValid && localRowsValid

  useEffect(() => {
    onValidityChange?.(valid)
  }, [onValidityChange, valid])

  const handleLocalValidityChange = useCallback((nextValid: boolean) => {
    setLocalRowsValid(nextValid)
  }, [])

  return (
    <div className='space-y-4'>
      <RouteGroupPricingTable
        {...props}
        onLocalValidityChange={handleLocalValidityChange}
      />
      <RouteGroupOverrideTable {...props} />
      <RouteGroupAutoEditor {...props} />
    </div>
  )
})
