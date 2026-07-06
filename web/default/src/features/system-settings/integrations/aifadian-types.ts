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
// ============================================================================
// Aifadian Payment Gateway Type Definitions
// ============================================================================

/**
 * Aifadian plan mapping configuration
 */
export interface AifadianPlan {
  /** Primary key ID */
  id: number
  /** Aifadian plan ID */
  plan_id: string
  /** Display name */
  name: string
  /** Plan type: subscription or topup */
  plan_type: 'subscription' | 'topup'
  /** For subscription type: bound subscription plan ID */
  subscription_plan_id: number
  /** SKU configuration JSON */
  sku_config: string
  /** Whether this plan is enabled */
  enabled: boolean
  /** Creation timestamp */
  created_at?: number
  /** Update timestamp */
  updated_at?: number
}

/**
 * API response for Aifadian plan operations
 */
export interface AifadianPlanResponse {
  success: boolean
  message?: string
  data?: AifadianPlan
}

/**
 * API response for list of Aifadian plans
 */
export interface AifadianPlansResponse {
  success: boolean
  message?: string
  data?: AifadianPlan[]
}

/**
 * Request to create a new Aifadian plan
 */
export interface CreateAifadianPlanRequest {
  plan_id: string
  name: string
  plan_type: 'subscription' | 'topup'
  subscription_plan_id: number
  sku_config: string
  enabled: boolean
}

/**
 * Request to update an existing Aifadian plan
 */
export interface UpdateAifadianPlanRequest extends CreateAifadianPlanRequest {}

/**
 * Response from Aifadian payment URL endpoint
 */
export interface AifadianPayUrlResponse {
  success: boolean
  message?: string
  data?: {
    url: string
    remark: string
  }
}
