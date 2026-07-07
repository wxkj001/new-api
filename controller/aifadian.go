package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// AifadianWebhookPayload represents the outer webhook envelope from Aifadian.
type AifadianWebhookPayload struct {
	EC   int                  `json:"ec"`
	EM   string               `json:"em"`
	Data AifadianWebhookData  `json:"data"`
}

// AifadianWebhookData is the inner data wrapper.
type AifadianWebhookData struct {
	Type  string              `json:"type"`
	Order AifadianOrderData   `json:"order"`
}

// AifadianOrderData is the order detail from Aifadian webhook.
type AifadianOrderData struct {
	OutTradeNo  string                   `json:"out_trade_no"`
	PlanID      string                   `json:"plan_id"`
	TotalAmount string                   `json:"total_amount"`
	Remark      string                   `json:"remark"`
	Status      int                      `json:"status"` // 2 = paid, 1 = pending
	Month       int                      `json:"month"`  // subscription duration in months
	SkuDetail   []map[string]interface{} `json:"sku_detail"`
}

// AifadianPayRequest is the request to get an Aifadian payment URL.
type AifadianPayRequest struct {
	PlanId string `json:"plan_id" form:"plan_id"` // Aifadian plan_id
	Month  int    `json:"month" form:"month"`     // Duration in months (default 1)
}

// parseAifadianRemark parses the remark field to extract username and user ID.
// Format: "用户名:wxkj;用户ID:1。请勿修改或者删除这里的信息以防充值不到账"
func parseAifadianRemark(remark string) (username string, userId int) {
	userId = 0
	parts := strings.Split(remark, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "用户名:") {
			username = strings.TrimPrefix(part, "用户名:")
			// Remove anything after special chars
			if idx := strings.IndexAny(username, "。，,."); idx >= 0 {
				username = username[:idx]
			}
			username = strings.TrimSpace(username)
		} else if strings.HasPrefix(part, "用户ID:") {
			idStr := strings.TrimPrefix(part, "用户ID:")
			if idx := strings.IndexAny(idStr, "。，,."); idx >= 0 {
				idStr = idStr[:idx]
			}
			idStr = strings.TrimSpace(idStr)
			if id, err := strconv.Atoi(idStr); err == nil {
				userId = id
			}
		}
	}
	return
}

// isAifadianWebhookEnabled checks if Aifadian payment is configured.
func isAifadianWebhookEnabled() bool {
	return true
}

// AifadianWebhook handles Aifadian payment callbacks.
// POST /api/aifadian/webhook
func AifadianWebhook(c *gin.Context) {
	if !isAifadianWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("爱发电 webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		c.JSON(http.StatusOK, gin.H{"ec": 200})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("爱发电 webhook 读取请求体失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		c.JSON(http.StatusOK, gin.H{"ec": 200})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("爱发电 webhook 收到请求 path=%q client_ip=%s body=%s", c.Request.RequestURI, c.ClientIP(), string(body)))

	var payload AifadianWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("爱发电 webhook 解析 JSON 失败 path=%q client_ip=%s error=%q body=%s", c.Request.RequestURI, c.ClientIP(), err.Error(), string(body)))
		c.JSON(http.StatusOK, gin.H{"ec": 200})
		return
	}

	if payload.EC != 200 {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("爱发电 webhook 业务错误 ec=%d em=%s", payload.EC, payload.EM))
		c.JSON(http.StatusOK, gin.H{"ec": 200})
		return
	}

	order := payload.Data.Order
	if order.OutTradeNo == "" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("爱发电 webhook 缺少 out_trade_no path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		c.JSON(http.StatusOK, gin.H{"ec": 200})
		return
	}

	// Check if already processed (idempotency)
	existingOrder := model.GetAifadianOrderByOrderId(order.OutTradeNo)
	if existingOrder != nil {
		if existingOrder.Processed {
			logger.LogInfo(c.Request.Context(), fmt.Sprintf("爱发电 webhook 订单已处理 order_id=%s", order.OutTradeNo))
			c.JSON(http.StatusOK, gin.H{"ec": 200})
			return
		}
		// Already exists but not processed - continue
	}

	// Parse user info from remark
	username, userId := parseAifadianRemark(order.Remark)
	if userId <= 0 {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("爱发电 webhook 无法从 remark 解析用户信息 order_id=%s remark=%s", order.OutTradeNo, order.Remark))
		// Save the order anyway for manual processing
		orderEntry := &model.AifadianOrder{
			OrderId:     order.OutTradeNo,
			UserId:      0,
			PlanId:      order.PlanID,
			Remark:      order.Remark,
			TotalAmount: order.TotalAmount,
			OrderStatus: fmt.Sprintf("%d", order.Status),
			Processed:   false,
			ProcessMsg:  "无法解析用户信息，需要手动处理",
		}
		_ = model.CreateAifadianOrder(orderEntry)
		c.JSON(http.StatusOK, gin.H{"ec": 200})
		return
	}

	// Verify user exists
	user, err := model.GetUserById(userId, false)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("爱发电 webhook 用户不存在 order_id=%s user_id=%d username=%s", order.OutTradeNo, userId, username))
		orderEntry := &model.AifadianOrder{
			OrderId:     order.OutTradeNo,
			UserId:      userId,
			PlanId:      order.PlanID,
			Remark:      order.Remark,
			TotalAmount: order.TotalAmount,
			OrderStatus: fmt.Sprintf("%d", order.Status),
			Processed:   false,
			ProcessMsg:  fmt.Sprintf("用户ID %d 不存在", userId),
		}
		_ = model.CreateAifadianOrder(orderEntry)
		c.JSON(http.StatusOK, gin.H{"ec": 200})
		return
	}

	// Only process "paid" status (2)
	if order.Status != 2 {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("爱发电 webhook 忽略非支付状态 order_id=%s status=%d", order.OutTradeNo, order.Status))
		orderEntry := &model.AifadianOrder{
			OrderId:     order.OutTradeNo,
			UserId:      userId,
			PlanId:      order.PlanID,
			Remark:      order.Remark,
			TotalAmount: order.TotalAmount,
			OrderStatus: fmt.Sprintf("%d", order.Status),
			Processed:   false,
			ProcessMsg:  fmt.Sprintf("非支付状态: %d", order.Status),
		}
		_ = model.CreateAifadianOrder(orderEntry)
		c.JSON(http.StatusOK, gin.H{"ec": 200})
		return
	}

	// Look up the Aifadian plan — SKU match first if present, then fall back to plan_id only
	var aifadianPlan *model.AifadianPlan
	if len(order.SkuDetail) > 0 {
		skuJson := common.GetJsonString(order.SkuDetail)
		aifadianPlan, err = model.GetAifadianPlanByPlanIdAndSku(order.PlanID, skuJson)
	}
	if aifadianPlan == nil {
		aifadianPlan, err = model.GetAifadianPlanByPlanId(order.PlanID)
	}
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("爱发电 webhook 未找到 plan_id order_id=%s plan_id=%s", order.OutTradeNo, order.PlanID))
		// Save order for manual processing
		orderEntry := &model.AifadianOrder{
			OrderId:     order.OutTradeNo,
			UserId:      userId,
			PlanId:      order.PlanID,
			Remark:      order.Remark,
			TotalAmount: order.TotalAmount,
			OrderStatus: fmt.Sprintf("%d", order.Status),
			Processed:   false,
			ProcessMsg:  fmt.Sprintf("未找到 plan_id: %s 的映射配置", order.PlanID),
		}
		_ = model.CreateAifadianOrder(orderEntry)
		c.JSON(http.StatusOK, gin.H{"ec": 200})
		return
	}

	if !aifadianPlan.Enabled {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("爱发电 webhook plan_id 未启用 order_id=%s plan_id=%s", order.OutTradeNo, order.PlanID))
		orderEntry := &model.AifadianOrder{
			OrderId:     order.OutTradeNo,
			UserId:      userId,
			PlanId:      order.PlanID,
			Remark:      order.Remark,
			TotalAmount: order.TotalAmount,
			OrderStatus: fmt.Sprintf("%d", order.Status),
			Processed:   false,
			ProcessMsg:  "该套餐未启用",
		}
		_ = model.CreateAifadianOrder(orderEntry)
		c.JSON(http.StatusOK, gin.H{"ec": 200})
		return
	}

	// Process the order
	var processMsg string
	var processOk bool
	var createdOrder *model.AifadianOrder

	if aifadianPlan.PlanType == "subscription" && aifadianPlan.SubscriptionPlanId > 0 {
		// Process as subscription - need to create a subscription order and complete it
		processOk, processMsg = processAifadianSubscriptionOrder(user.Id, aifadianPlan, order)
		if !processOk {
			logger.LogError(c.Request.Context(), fmt.Sprintf("爱发电 webhook 处理订阅订单失败 order_id=%s user_id=%d plan_id=%s error=%s", order.OutTradeNo, userId, order.PlanID, processMsg))
		} else {
			logger.LogInfo(c.Request.Context(), fmt.Sprintf("爱发电 webhook 订阅订单成功 order_id=%s user_id=%d plan_id=%s", order.OutTradeNo, userId, order.PlanID))
		}
	} else if aifadianPlan.PlanType == "topup" {
		// Calculate quota from payment amount
		amountFloat, _ := strconv.ParseFloat(order.TotalAmount, 64)
		quota := int(aifadianPlan.QuotaAmount)
		if quota <= 0 {
			// Dynamic: convert payment amount to quota using system rate
			quota = int(amountFloat / common.QuotaPerUnit)
			if quota <= 0 {
				quota = 1
			}
		}
		err = model.IncreaseUserQuota(userId, quota, true)
		if err != nil {
			processOk = false
			processMsg = fmt.Sprintf("增加额度失败: %s", err.Error())
			logger.LogError(c.Request.Context(), fmt.Sprintf("爱发电 webhook 增加额度失败 order_id=%s user_id=%d plan_id=%s quota=%d error=%s", order.OutTradeNo, userId, order.PlanID, quota, err.Error()))
		} else {
			processOk = true
			processMsg = fmt.Sprintf("充值成功，增加额度: %d (支付金额: %s)", quota, order.TotalAmount)
			model.RecordTopupLog(userId, fmt.Sprintf("爱发电充值成功，订单: %s，充值金额: %s，获取额度: %d", order.OutTradeNo, order.TotalAmount, quota), c.ClientIP(), "aifadian", "aifadian")
			logger.LogInfo(c.Request.Context(), fmt.Sprintf("爱发电 webhook 充值成功 order_id=%s user_id=%d plan_id=%s quota=%d", order.OutTradeNo, userId, order.PlanID, quota))
		}
	} else {
		processMsg = fmt.Sprintf("plan_type=%s 配置无效", aifadianPlan.PlanType)
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("爱发电 webhook 配置无效 order_id=%s plan_id=%s plan_type=%s", order.OutTradeNo, order.PlanID, aifadianPlan.PlanType))
	}

	// Save the order
	createdOrder = &model.AifadianOrder{
		OrderId:     order.OutTradeNo,
		UserId:      userId,
		PlanId:      order.PlanID,
		Remark:      order.Remark,
		TotalAmount: order.TotalAmount,
		OrderStatus: fmt.Sprintf("%d", order.Status),
		Processed:   processOk,
		ProcessMsg:  processMsg,
		CompleteTime: func() int64 {
			if processOk {
				return time.Now().Unix()
			}
			return 0
		}(),
	}

	// Use existing or create new
	if existingOrder != nil {
		existingOrder.Processed = processOk
		existingOrder.ProcessMsg = processMsg
		if processOk {
			existingOrder.CompleteTime = time.Now().Unix()
		}
		_ = model.UpdateAifadianOrder(existingOrder)
	} else {
		_ = model.CreateAifadianOrder(createdOrder)
	}

	c.JSON(http.StatusOK, gin.H{"ec": 200})
}

// processAifadianSubscriptionOrder creates a subscription order and completes it.
func processAifadianSubscriptionOrder(userId int, aifadianPlan *model.AifadianPlan, order AifadianOrderData) (bool, string) {
	// Get the subscription plan
	subPlan, err := model.GetSubscriptionPlanById(aifadianPlan.SubscriptionPlanId)
	if err != nil {
		return false, fmt.Sprintf("未找到订阅套餐 ID %d", aifadianPlan.SubscriptionPlanId)
	}

	// Create trade no based on aifadian out_trade_no
	tradeNo := "AFD" + order.OutTradeNo
	if len(tradeNo) > 255 {
		tradeNo = tradeNo[:255]
	}

	// Parse amount
	amount := 0.0
	if f, err := strconv.ParseFloat(order.TotalAmount, 64); err == nil {
		amount = f
	}

	// Create subscription order
	subOrder := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          subPlan.Id,
		Money:           amount,
		TradeNo:         tradeNo,
		PaymentMethod:   "aifadian",
		PaymentProvider: model.PaymentProviderEpay, // reuse as generic
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	if err := subOrder.Insert(); err != nil {
		return false, fmt.Sprintf("创建订阅订单失败: %s", err.Error())
	}

	// Complete the subscription order
	if err := model.CompleteSubscriptionOrder(tradeNo, common.GetJsonString(order), model.PaymentProviderEpay, "aifadian"); err != nil {
		return false, fmt.Sprintf("完成订阅订单失败: %s", err.Error())
	}

	// Extend subscription duration based on Afdian month count
	if order.Month > 1 {
		// Find the subscription just created and extend its end_time
		addMonths := order.Month - 1
		if err := model.ExtendUserSubscriptionEndTime(userId, subPlan.Id, addMonths); err != nil {
			// Non-fatal: the subscription was already created with 1 month
			logger.LogWarn(context.Background(), fmt.Sprintf("爱发电 扩展订阅时长失败 user_id=%d plan_id=%d months=%d error=%s", userId, subPlan.Id, addMonths, err.Error()))
		}
	}

	return true, fmt.Sprintf("订阅套餐 '%s' 激活成功", subPlan.Title)
}

// AifadianPayURL returns the Aifadian payment URL with user info in the remark.
// GET /api/topup/aifadian
func AifadianPayURL(c *gin.Context) {
	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	planId := c.Query("plan_id")
	if planId == "" {
		common.ApiErrorMsg(c, "缺少 plan_id 参数")
		return
	}

	skuParam := c.Query("sku")

	monthStr := c.DefaultQuery("month", "1")
	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 {
		month = 1
	}

	// Look up the plan — SKU match first if provided
	var aifadianPlan *model.AifadianPlan
	if skuParam != "" {
		aifadianPlan, err = model.GetAifadianPlanByPlanIdAndSku(planId, skuParam)
	}
	if aifadianPlan == nil {
		aifadianPlan, err = model.GetAifadianPlanByPlanId(planId)
	}
	hasSku := aifadianPlan != nil && strings.TrimSpace(aifadianPlan.SkuConfig) != ""

	// Build remark: "用户名:wxkj;用户ID:1。请勿修改或者删除这里的信息以防充值不到账"
	remark := fmt.Sprintf("用户名:%s;用户ID:%d。请勿修改或者删除这里的信息以防充值不到账", user.Username, userId)

	// Build the Aifadian payment URL
	v := url.Values{}
	v.Set("plan_id", planId)
	if hasSku {
		v.Set("product_type", "1")
		v.Set("sku", strings.TrimSpace(aifadianPlan.SkuConfig))
	} else {
		v.Set("product_type", "0")
	}
	v.Set("month", strconv.Itoa(month))
	v.Set("remark", remark)

	payUrl := "https://ifdian.net/order/create?" + v.Encode()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"url":    payUrl,
			"remark": remark,
		},
	})
}

// Admin endpoints for Aifadian plan management

// AdminGetAifadianPlans returns all Aifadian plan mappings.
// GET /api/aifadian/plans
func AdminGetAifadianPlans(c *gin.Context) {
	plans, err := model.GetAllAifadianPlans()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if plans == nil {
		plans = []*model.AifadianPlan{}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    plans,
	})
}

type adminCreateAifadianPlanRequest struct {
	PlanId             string `json:"plan_id"`
	Name               string `json:"name"`
	PlanType           string `json:"plan_type"` // subscription or topup
	SubscriptionPlanId int    `json:"subscription_plan_id"`
	QuotaAmount        int64  `json:"quota_amount"`
	SkuConfig          string `json:"sku_config"`
	Enabled            bool   `json:"enabled"`
}

// AdminCreateAifadianPlan creates a new Aifadian plan mapping.
// POST /api/aifadian/plans
func AdminCreateAifadianPlan(c *gin.Context) {
	var req adminCreateAifadianPlanRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "请求参数错误")
		return
	}
	if req.PlanId == "" {
		common.ApiErrorMsg(c, "plan_id 不能为空")
		return
	}
	if req.PlanType != "subscription" && req.PlanType != "topup" {
		common.ApiErrorMsg(c, "plan_type 必须是 subscription 或 topup")
		return
	}

	plan := &model.AifadianPlan{
		PlanId:             req.PlanId,
		Name:               req.Name,
		PlanType:           req.PlanType,
		SubscriptionPlanId: req.SubscriptionPlanId,
		QuotaAmount:        req.QuotaAmount,
		SkuConfig:          req.SkuConfig,
		Enabled:            req.Enabled,
	}
	if err := model.CreateAifadianPlan(plan); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    plan,
	})
}

// AdminUpdateAifadianPlan updates an existing Aifadian plan mapping.
// PUT /api/aifadian/plans/:id
func AdminUpdateAifadianPlan(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的 ID")
		return
	}

	existing, err := model.GetAifadianPlanById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var req adminCreateAifadianPlanRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "请求参数错误")
		return
	}
	if req.PlanId == "" {
		common.ApiErrorMsg(c, "plan_id 不能为空")
		return
	}
	if req.PlanType != "subscription" && req.PlanType != "topup" {
		common.ApiErrorMsg(c, "plan_type 必须是 subscription 或 topup")
		return
	}

	// PlanId is immutable after creation
	existing.Name = req.Name
	existing.PlanType = req.PlanType
	existing.SubscriptionPlanId = req.SubscriptionPlanId
	existing.QuotaAmount = req.QuotaAmount
	existing.SkuConfig = req.SkuConfig
	existing.Enabled = req.Enabled

	if err := model.UpdateAifadianPlan(existing); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    existing,
	})
}

// AdminDeleteAifadianPlan deletes an Aifadian plan mapping.
// DELETE /api/aifadian/plans/:id
func AdminDeleteAifadianPlan(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的 ID")
		return
	}
	if err := model.DeleteAifadianPlan(id); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

// AdminGetAifadianOrders returns Aifadian order records.
// GET /api/aifadian/orders
func AdminGetAifadianOrders(c *gin.Context) {
	// For simplicity, return orders from the model package
	// We'll add a proper query later
	type orderRow struct {
		Order model.AifadianOrder `json:"order"`
	}
	_ = orderRow{}
	common.ApiErrorMsg(c, "暂未实现完整列表查询")
}
