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

import type {
  AifadianPlanResponse,
  AifadianPlansResponse,
  CreateAifadianPlanRequest,
  UpdateAifadianPlanRequest,
} from './aifadian-types'

// ============================================================================
// Admin Aifadian Plan Management API
// ============================================================================

/**
 * Get all Aifadian plan mappings
 */
export async function getAifadianPlans(): Promise<AifadianPlansResponse> {
  const res = await api.get('/api/aifadian/plans')
  return res.data
}

/**
 * Create a new Aifadian plan mapping
 */
export async function createAifadianPlan(
  data: CreateAifadianPlanRequest
): Promise<AifadianPlanResponse> {
  const res = await api.post('/api/aifadian/plans', data)
  return res.data
}

/**
 * Update an existing Aifadian plan mapping
 */
export async function updateAifadianPlan(
  id: number,
  data: UpdateAifadianPlanRequest
): Promise<AifadianPlanResponse> {
  const res = await api.put(`/api/aifadian/plans/${id}`, data)
  return res.data
}

/**
 * Delete an Aifadian plan mapping
 */
export async function deleteAifadianPlan(
  id: number
): Promise<AifadianPlanResponse> {
  const res = await api.delete(`/api/aifadian/plans/${id}`)
  return res.data
}

/**
 * Navigate to Aifadian payment page (302 redirect).
 * Opens in a new tab so the user doesn't leave the current page.
 */
export async function getAifadianPayUrl(
  planId: string,
  month: number = 1
): Promise<{
  success: boolean
  message?: string
  data?: { url: string; remark: string }
}> {
  const res = await api.get('/api/user/topup/aifadian', {
    params: { plan_id: planId, month },
  })
  return res.data
}
