package model

import (
	"errors"
	"time"
)

// AifadianPlan binds an Aifadian plan_id to either a subscription plan or direct topup quota.
type AifadianPlan struct {
	Id        int    `json:"id" gorm:"primaryKey"`
	PlanId    string `json:"plan_id" gorm:"type:varchar(128);uniqueIndex;not null"` // Aifadian plan_id
	Name      string `json:"name" gorm:"type:varchar(128);default:''"`              // Display name
	PlanType  string `json:"plan_type" gorm:"type:varchar(16);not null;default:'subscription'"` // "subscription" or "topup"

	// For subscription binding
	SubscriptionPlanId int `json:"subscription_plan_id" gorm:"default:0"`
	// For topup binding (quota amount)
	QuotaAmount int64 `json:"quota_amount" gorm:"type:bigint;default:0"`
	// SKU configuration JSON: [{"sku_id":"xxx","count":1}]
	SkuConfig string `json:"sku_config" gorm:"type:text;default:''"`

	Enabled   bool  `json:"enabled" gorm:"default:true"`
	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (AifadianPlan) TableName() string {
	return "aifadian_plans"
}

func (p *AifadianPlan) BeforeCreate() error {
	now := time.Now().Unix()
	p.CreatedAt = now
	p.UpdatedAt = now
	return nil
}

func (p *AifadianPlan) BeforeUpdate() error {
	p.UpdatedAt = time.Now().Unix()
	return nil
}

// AifadianOrder stores order info from Aifadian payment callbacks.
type AifadianOrder struct {
	Id           int    `json:"id" gorm:"primaryKey"`
	OrderId      string `json:"order_id" gorm:"type:varchar(128);uniqueIndex;not null"` // Aifadian order ID
	UserId       int    `json:"user_id" gorm:"index;default:0"`
	PlanId       string `json:"plan_id" gorm:"type:varchar(128);index;not null"` // Aifadian plan_id
	Remark       string `json:"remark" gorm:"type:text"`
	TotalAmount  string `json:"total_amount" gorm:"type:varchar(32)"`    // Payment amount string
	OrderStatus  string `json:"order_status" gorm:"type:varchar(32)"`    // paid, cancelled, etc.
	Processed    bool   `json:"processed" gorm:"default:false"`          // Whether the order has been processed
	ProcessMsg   string `json:"process_msg" gorm:"type:varchar(255)"`    // Processing result message
	ProcessedAt  int64  `json:"processed_at" gorm:"bigint;default:0"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint"`
	CompleteTime int64  `json:"complete_time" gorm:"bigint;default:0"`
}

func (AifadianOrder) TableName() string {
	return "aifadian_orders"
}

func (o *AifadianOrder) BeforeCreate() error {
	o.CreatedAt = time.Now().Unix()
	return nil
}

// CRUD for AifadianPlan

func GetAllAifadianPlans() ([]*AifadianPlan, error) {
	var plans []*AifadianPlan
	err := DB.Order("id asc").Find(&plans).Error
	return plans, err
}

func GetEnabledAifadianPlans() ([]*AifadianPlan, error) {
	var plans []*AifadianPlan
	err := DB.Where("enabled = ?", true).Order("id asc").Find(&plans).Error
	return plans, err
}

func GetAifadianPlanById(id int) (*AifadianPlan, error) {
	var plan AifadianPlan
	err := DB.First(&plan, id).Error
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

func GetAifadianPlanByPlanId(planId string) (*AifadianPlan, error) {
	var plan AifadianPlan
	err := DB.Where("plan_id = ?", planId).First(&plan).Error
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

func CreateAifadianPlan(plan *AifadianPlan) error {
	if plan.PlanId == "" {
		return errors.New("plan_id is required")
	}
	return DB.Create(plan).Error
}

func UpdateAifadianPlan(plan *AifadianPlan) error {
	if plan.PlanId == "" {
		return errors.New("plan_id is required")
	}
	return DB.Save(plan).Error
}

func DeleteAifadianPlan(id int) error {
	return DB.Delete(&AifadianPlan{}, id).Error
}

// CRUD for AifadianOrder

func GetAifadianOrderByOrderId(orderId string) *AifadianOrder {
	var order AifadianOrder
	err := DB.Where("order_id = ?", orderId).First(&order).Error
	if err != nil {
		return nil
	}
	return &order
}

func CreateAifadianOrder(order *AifadianOrder) error {
	return DB.Create(order).Error
}

func UpdateAifadianOrder(order *AifadianOrder) error {
	return DB.Save(order).Error
}
