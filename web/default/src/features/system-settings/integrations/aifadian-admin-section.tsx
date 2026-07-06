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
import { Pencil, Plus, Search, Trash2 } from 'lucide-react'
import { useState, useMemo, useCallback, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { cn } from '@/lib/utils'

import {
  getAifadianPlans,
  createAifadianPlan,
  updateAifadianPlan,
  deleteAifadianPlan,
} from './aifadian-api'
import type {
  AifadianPlan,
  CreateAifadianPlanRequest,
} from './aifadian-types'
import { getAdminPlans } from '@/features/subscriptions/api'
import type { PlanRecord } from '@/features/subscriptions/types'

// ============================================================================
// Types & Schema
// ============================================================================

const aifadianPlanSchema = z.object({
  plan_id: z.string().min(1),
  name: z.string().min(1),
  plan_type: z.enum(['subscription', 'topup']),
  subscription_plan_id: z.string(),
  sku_config: z.string(),
  enabled: z.boolean(),
})

type AifadianPlanFormValues = z.infer<typeof aifadianPlanSchema>

// ============================================================================
// Dialog Component
// ============================================================================

const AIFADIAN_PLAN_FORM_ID = 'aifadian-plan-form'

type AifadianPlanDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (data: CreateAifadianPlanRequest) => void
  editData?: AifadianPlan | null
  isLoading?: boolean
}

function AifadianPlanDialog({
  open,
  onOpenChange,
  onSave,
  editData,
  isLoading,
}: AifadianPlanDialogProps) {
  const { t } = useTranslation()
  const isEditMode = !!editData

  const { data: plansData } = useQuery({
    queryKey: ['subscription-plans-admin'],
    queryFn: getAdminPlans,
    enabled: open,
  })
  const plans = plansData?.data ?? []

  const form = useForm<AifadianPlanFormValues>({
    resolver: zodResolver(aifadianPlanSchema),
    defaultValues: {
      plan_id: '',
      name: '',
      plan_type: 'topup',
      subscription_plan_id: '',
      sku_config: '',
      enabled: true,
    },
  })

  // Reset form when dialog opens/closes or editData changes
  const resetForm = useCallback(() => {
    if (editData) {
      form.reset({
        plan_id: editData.plan_id,
        name: editData.name,
        plan_type: editData.plan_type,
        subscription_plan_id: String(editData.subscription_plan_id),
        sku_config: editData.sku_config || '',
        enabled: editData.enabled,
      })
    } else {
      form.reset({
        plan_id: '',
        name: '',
        plan_type: 'topup',
        subscription_plan_id: '',
        sku_config: '',
        enabled: true,
      })
    }
  }, [editData, form])

  // Reset form when dialog opens
  useEffect(() => {
    if (open) resetForm()
  }, [open, resetForm])

  // Watch plan_type to conditionally enable fields
  const planType = form.watch('plan_type')
  const selectedPlanId = form.watch('subscription_plan_id')

  const selectedPlanName = useMemo(() => {
    if (!selectedPlanId) return undefined
    const p = plans.find((x: PlanRecord) => String(x.plan.id) === selectedPlanId)
    return p ? p.plan.title : undefined
  }, [plans, selectedPlanId])

  const handleSubmit = (values: AifadianPlanFormValues) => {
    onSave({
      ...values,
      subscription_plan_id: parseInt(values.subscription_plan_id) || 0,
    })
    onOpenChange(false)
    form.reset()
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(newOpen) => {
        onOpenChange(newOpen)
        if (!newOpen) {
          setTimeout(resetForm, 200)
        }
      }}
    >
      <DialogContent className='sm:max-w-[500px]'>
        <DialogHeader>
          <DialogTitle>
            {isEditMode ? t('Edit Aifadian Plan') : t('Add Aifadian Plan')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Configure an Aifadian plan mapping for subscription or topup payments.'
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='-mx-4 -my-2 space-y-4 px-4'> 
      <Form {...form}>
        <form
          id={AIFADIAN_PLAN_FORM_ID}
          onSubmit={form.handleSubmit(handleSubmit)}
          className='space-y-4'
        >
          <FormField
            control={form.control}
            name='plan_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Aifadian Plan ID')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder={t('e.g., plan_xxxxxxxx')}
                    disabled={isEditMode}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'The Aifadian plan ID from your 爱发电 dashboard.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='name'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Display Name')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder={t('e.g., Monthly Support')}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t('Display name shown to users.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='plan_type'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Plan Type')}</FormLabel>
                <Select
                  items={[
                    { value: 'subscription', label: t('Subscription') },
                    { value: 'topup', label: t('Topup') },
                  ]}
                  onValueChange={field.onChange}
                  value={field.value}
                >
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue placeholder={t('Select plan type')} />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectItem value='subscription'>
                        {t('Subscription')}
                      </SelectItem>
                      <SelectItem value='topup'>{t('Topup')}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormDescription>
                  {t(
                    'Subscription plans create recurring billing, while topup plans add quota directly.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='subscription_plan_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Subscription Plan')}</FormLabel>
                <FormControl>
                  <Select
                    disabled={planType !== 'subscription'}
                    value={field.value}
                    onValueChange={field.onChange}
                  >
                    <SelectTrigger
                      className={cn(
                        planType !== 'subscription' && 'opacity-50'
                      )}
                    >
                      <SelectValue placeholder={t('Select a plan')}>
                        {selectedPlanName}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {plans.map((p: PlanRecord) => (
                        <SelectItem
                          key={p.plan.id}
                          value={String(p.plan.id)}
                        >
                          {p.plan.title}
                          {p.plan.enabled ? '' : ` (${t('Disabled')})`}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </FormControl>
                <FormDescription>
                  {t(
                    'The subscription plan to activate when this Aifadian plan is purchased.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='sku_config'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('SKU Config')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder='[{"sku_id":"xxx","count":1}]'
                    autoComplete='off'
                    value={field.value ?? ''}
                    onChange={(event) =>
                      field.onChange(event.target.value)
                    }
                    name={field.name}
                    onBlur={field.onBlur}
                    ref={field.ref}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'JSON array of SKU items. Leave empty for product-type 0.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <FormItem>
                <div className='flex items-center gap-3'>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                  <FormLabel className='!mb-0'>
                    {t('Enabled')}
                  </FormLabel>
                </div>
                <FormDescription>
                  {t('Disabled plans will not be shown to users.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
        </div>
        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form={AIFADIAN_PLAN_FORM_ID}
            disabled={isLoading}
          >
            {isLoading
              ? t('Saving...')
              : isEditMode
                ? t('Update')
                : t('Add')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ============================================================================
// Admin Section Component
// ============================================================================

export function AifadianSettingsSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [searchText, setSearchText] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editData, setEditData] = useState<AifadianPlan | null>(null)

  // Fetch plans
  const {
    data: plansData,
    isLoading,
    isError,
    error,
  } = useQuery({
    queryKey: ['aifadian-plans'],
    queryFn: getAifadianPlans,
  })

  const plans = useMemo(() => {
    return plansData?.data ?? []
  }, [plansData])

  // Create mutation
  const createMutation = useMutation({
    mutationFn: createAifadianPlan,
    onSuccess: (response) => {
      if (response.success) {
        toast.success(t('Aifadian plan created successfully'))
        queryClient.invalidateQueries({ queryKey: ['aifadian-plans'] })
      } else {
        toast.error(response.message || t('Failed to create Aifadian plan'))
      }
    },
    onError: (err: Error) => {
      toast.error(err.message || t('Failed to create Aifadian plan'))
    },
  })

  // Update mutation
  const updateMutation = useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id: number
      data: CreateAifadianPlanRequest
    }) => updateAifadianPlan(id, data),
    onSuccess: (response) => {
      if (response.success) {
        toast.success(t('Aifadian plan updated successfully'))
        queryClient.invalidateQueries({ queryKey: ['aifadian-plans'] })
      } else {
        toast.error(response.message || t('Failed to update Aifadian plan'))
      }
    },
    onError: (err: Error) => {
      toast.error(err.message || t('Failed to update Aifadian plan'))
    },
  })

  // Delete mutation
  const deleteMutation = useMutation({
    mutationFn: deleteAifadianPlan,
    onSuccess: (response) => {
      if (response.success) {
        toast.success(t('Aifadian plan deleted successfully'))
        queryClient.invalidateQueries({ queryKey: ['aifadian-plans'] })
      } else {
        toast.error(response.message || t('Failed to delete Aifadian plan'))
      }
    },
    onError: (err: Error) => {
      toast.error(err.message || t('Failed to delete Aifadian plan'))
    },
  })

  const filteredPlans = useMemo(() => {
    if (!searchText) return plans
    const lowerSearch = searchText.toLowerCase()
    return plans.filter(
      (plan) =>
        plan.plan_id.toLowerCase().includes(lowerSearch) ||
        plan.name.toLowerCase().includes(lowerSearch)
    )
  }, [plans, searchText])

  const handleSave = (data: CreateAifadianPlanRequest) => {
    if (editData) {
      updateMutation.mutate({ id: editData.id, data })
    } else {
      createMutation.mutate(data)
    }
  }

  const handleEdit = (plan: AifadianPlan) => {
    setEditData(plan)
    setDialogOpen(true)
  }

  const handleAdd = () => {
    setEditData(null)
    setDialogOpen(true)
  }

  const handleDelete = (plan: AifadianPlan) => {
    if (
      window.confirm(
        t('Are you sure you want to delete this Aifadian plan?')
      )
    ) {
      deleteMutation.mutate(plan.id)
    }
  }

  const isSaving =
    createMutation.isPending || updateMutation.isPending

  if (isError) {
    return (
      <div className='text-destructive p-4'>
        {t('Failed to load Aifadian plans')}:{' '}
        {error instanceof Error ? error.message : String(error)}
      </div>
    )
  }

  return (
    <div className='space-y-4'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-center'>
        <div className='relative flex-1'>
          <Search className='text-muted-foreground absolute top-2.5 left-2.5 h-4 w-4' />
          <Input
            placeholder={t('Search plans...')}
            value={searchText}
            onChange={(e) => setSearchText(e.target.value)}
            className='pl-9'
          />
        </div>
        <Button
          type='button'
          onClick={(e) => {
            e.preventDefault()
            e.stopPropagation()
            handleAdd()
          }}
          className='flex-1 sm:flex-none'
        >
          <Plus className='h-4 w-4 sm:mr-2' />
          <span className='sm:inline'>{t('Add plan')}</span>
        </Button>
      </div>

      {isLoading ? (
        <div className='text-muted-foreground p-8 text-center'>
          {t('Loading...')}
        </div>
      ) : filteredPlans.length === 0 ? (
        <div className='text-muted-foreground rounded-lg border border-dashed p-8 text-center text-sm'>
          {searchText
            ? t('No plans match your search')
            : t(
                'No Aifadian plans configured. Click "Add plan" to get started.'
              )}
        </div>
      ) : (
        <div className='rounded-md border'>
          {/* Desktop table view */}
          <StaticDataTable
            className='hidden rounded-none border-0 md:block'
            data={filteredPlans}
            getRowKey={(plan) => String(plan.id)}
            columns={[
              {
                id: 'plan_id',
                header: t('Plan ID'),
                cell: (plan) => (
                  <code className='bg-muted rounded px-1.5 py-0.5 text-sm'>
                    {plan.plan_id}
                  </code>
                ),
              },
              {
                id: 'name',
                header: t('Name'),
                cellClassName: 'font-medium',
                cell: (plan) => plan.name,
              },
              {
                id: 'plan_type',
                header: t('Type'),
                cell: (plan) => (
                  <span
                    className={cn(
                      'rounded px-2 py-0.5 text-xs',
                      plan.plan_type === 'subscription'
                        ? 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200'
                        : 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
                    )}
                  >
                    {plan.plan_type === 'subscription'
                      ? t('Subscription')
                      : t('Topup')}
                  </span>
                ),
              },
              {
                id: 'value',
                header: t('Value'),
                cell: (plan) => (
                  <span className='font-mono text-sm'>
                    {plan.plan_type === 'subscription'
                      ? `Plan #${plan.subscription_plan_id}`
                      : t('Dynamic')}
                  </span>
                ),
              },
              {
                id: 'enabled',
                header: t('Enabled'),
                cell: (plan) => (
                  <span
                    className={cn(
                      'rounded px-2 py-0.5 text-xs',
                      plan.enabled
                        ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
                        : 'bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-200'
                    )}
                  >
                    {plan.enabled ? t('Yes') : t('No')}
                  </span>
                ),
              },
              {
                id: 'actions',
                header: t('Actions'),
                className: 'text-right',
                cellClassName: 'text-right',
                cell: (plan) => (
                  <StaticRowActions
                    editLabel={t('Edit')}
                    deleteLabel={t('Delete')}
                    menuLabel={t('Open menu')}
                    onEdit={() => handleEdit(plan)}
                    onDelete={() => handleDelete(plan)}
                  />
                ),
              },
            ]}
          />

          {/* Mobile card view */}
          <div className='divide-y md:hidden'>
            {filteredPlans.map((plan) => (
              <div key={plan.id} className='p-4'>
                <div className='mb-3 flex items-start justify-between'>
                  <div className='flex-1'>
                    <div className='mb-1 font-medium'>{plan.name}</div>
                    <code className='bg-muted rounded px-1.5 py-0.5 text-xs'>
                      {plan.plan_id}
                    </code>
                  </div>
                  <div className='flex gap-1'>
                    <Button
                      type='button'
                      variant='ghost'
                      size='sm'
                      onClick={(e) => {
                        e.preventDefault()
                        e.stopPropagation()
                        handleEdit(plan)
                      }}
                    >
                      <Pencil className='h-4 w-4' />
                    </Button>
                    <Button
                      type='button'
                      variant='ghost'
                      size='sm'
                      onClick={(e) => {
                        e.preventDefault()
                        e.stopPropagation()
                        handleDelete(plan)
                      }}
                    >
                      <Trash2 className='h-4 w-4' />
                    </Button>
                  </div>
                </div>
                <div className='space-y-2 text-sm'>
                  <div className='flex items-center gap-2'>
                    <span className='text-muted-foreground min-w-16'>
                      {t('Type')}:
                    </span>
                    <span
                      className={cn(
                        'rounded px-2 py-0.5 text-xs',
                        plan.plan_type === 'subscription'
                          ? 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200'
                          : 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
                      )}
                    >
                      {plan.plan_type === 'subscription'
                        ? t('Subscription')
                        : t('Topup')}
                    </span>
                  </div>
                  <div className='flex items-center gap-2'>
                    <span className='text-muted-foreground min-w-16'>
                      {t('Value')}:
                    </span>
                    <span className='font-mono'>
                      {plan.plan_type === 'subscription'
                        ? `Plan #${plan.subscription_plan_id}`
                        : t('Dynamic')}
                    </span>
                  </div>
                  <div className='flex items-center gap-2'>
                    <span className='text-muted-foreground min-w-16'>
                      {t('Enabled')}:
                    </span>
                    <span
                      className={cn(
                        'rounded px-2 py-0.5 text-xs',
                        plan.enabled
                          ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
                          : 'bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-200'
                      )}
                    >
                      {plan.enabled ? t('Yes') : t('No')}
                    </span>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      <AifadianPlanDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onSave={handleSave}
        editData={editData}
        isLoading={isSaving}
      />
    </div>
  )
}
