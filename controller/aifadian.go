package controller

import (
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
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// AifadianCallbackPayload represents the webhook payload from Aifadian.
type AifadianCallbackPayload struct {
	OrderID     string `json:"order_id"`
	PlanID      string `json:"plan_id"`
	TotalAmount string `json:"total_amount"`
	Remark      string `json:"remark"`
	Status      string `json:"status"`
	CreateTime  string `json:"create_time"`
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
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "webhook disabled"})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("爱发电 webhook 读取请求体失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "read body failed"})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("爱发电 webhook 收到请求 path=%q client_ip=%s body=%s", c.Request.RequestURI, c.ClientIP(), string(body)))

	var payload AifadianCallbackPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("爱发电 webhook 解析 JSON 失败 path=%q client_ip=%s error=%q body=%s", c.Request.RequestURI, c.ClientIP(), err.Error(), string(body)))
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "invalid json"})
		return
	}

	if payload.OrderID == "" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("爱发电 webhook 缺少 order_id path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "missing order_id"})
		return
	}

	// Check if already processed (idempotency)
	existingOrder := model.GetAifadianOrderByOrderId(payload.OrderID)
	if existingOrder != nil {
		if existingOrder.Processed {
			logger.LogInfo(c.Request.Context(), fmt.Sprintf("爱发电 webhook 订单已处理 order_id=%s", payload.OrderID))
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "already processed"})
			return
		}
		// Already exists but not processed - continue
	}

	// Parse user info from remark
	username, userId := parseAifadianRemark(payload.Remark)
	if userId <= 0 {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("爱发电 webhook 无法从 remark 解析用户信息 order_id=%s remark=%s", payload.OrderID, payload.Remark))
		// Save the order anyway for manual processing
		order := &model.AifadianOrder{
			OrderId:     payload.OrderID,
			UserId:      0,
			PlanId:      payload.PlanID,
			Remark:      payload.Remark,
			TotalAmount: payload.TotalAmount,
			OrderStatus: payload.Status,
			Processed:   false,
			ProcessMsg:  "无法解析用户信息，需要手动处理",
		}
		_ = model.CreateAifadianOrder(order)
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "received, but user not found"})
		return
	}

	// Verify user exists
	user, err := model.GetUserById(userId, false)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("爱发电 webhook 用户不存在 order_id=%s user_id=%d username=%s", payload.OrderID, userId, username))
		order := &model.AifadianOrder{
			OrderId:     payload.OrderID,
			UserId:      userId,
			PlanId:      payload.PlanID,
			Remark:      payload.Remark,
			TotalAmount: payload.TotalAmount,
			OrderStatus: payload.Status,
			Processed:   false,
			ProcessMsg:  fmt.Sprintf("用户ID %d 不存在", userId),
		}
		_ = model.CreateAifadianOrder(order)
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "user not found"})
		return
	}

	// Only process "paid" status
	if payload.Status != "paid" {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("爱发电 webhook 忽略非支付状态 order_id=%s status=%s", payload.OrderID, payload.Status))
		order := &model.AifadianOrder{
			OrderId:     payload.OrderID,
			UserId:      userId,
			PlanId:      payload.PlanID,
			Remark:      payload.Remark,
			TotalAmount: payload.TotalAmount,
			OrderStatus: payload.Status,
			Processed:   false,
			ProcessMsg:  fmt.Sprintf("非支付状态: %s", payload.Status),
		}
		_ = model.CreateAifadianOrder(order)
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "received"})
		return
	}

	// Look up the Aifadian plan to determine what to do
	aifadianPlan, err := model.GetAifadianPlanByPlanId(payload.PlanID)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("爱发电 webhook 未找到 plan_id order_id=%s plan_id=%s", payload.OrderID, payload.PlanID))
		// Save order for manual processing
		order := &model.AifadianOrder{
			OrderId:     payload.OrderID,
			UserId:      userId,
			PlanId:      payload.PlanID,
			Remark:      payload.Remark,
			TotalAmount: payload.TotalAmount,
			OrderStatus: payload.Status,
			Processed:   false,
			ProcessMsg:  fmt.Sprintf("未找到 plan_id: %s 的映射配置", payload.PlanID),
		}
		_ = model.CreateAifadianOrder(order)
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "plan not configured"})
		return
	}

	if !aifadianPlan.Enabled {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("爱发电 webhook plan_id 未启用 order_id=%s plan_id=%s", payload.OrderID, payload.PlanID))
		order := &model.AifadianOrder{
			OrderId:     payload.OrderID,
			UserId:      userId,
			PlanId:      payload.PlanID,
			Remark:      payload.Remark,
			TotalAmount: payload.TotalAmount,
			OrderStatus: payload.Status,
			Processed:   false,
			ProcessMsg:  "该套餐未启用",
		}
		_ = model.CreateAifadianOrder(order)
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "plan disabled"})
		return
	}

	// Process the order
	var processMsg string
	var processOk bool
	var createdOrder *model.AifadianOrder

	if aifadianPlan.PlanType == "subscription" && aifadianPlan.SubscriptionPlanId > 0 {
		// Process as subscription - need to create a subscription order and complete it
		processOk, processMsg = processAifadianSubscriptionOrder(user.Id, aifadianPlan, payload)
		if !processOk {
			logger.LogError(c.Request.Context(), fmt.Sprintf("爱发电 webhook 处理订阅订单失败 order_id=%s user_id=%d plan_id=%s error=%s", payload.OrderID, userId, payload.PlanID, processMsg))
		} else {
			logger.LogInfo(c.Request.Context(), fmt.Sprintf("爱发电 webhook 订阅订单成功 order_id=%s user_id=%d plan_id=%s", payload.OrderID, userId, payload.PlanID))
		}
	} else if aifadianPlan.PlanType == "topup" && aifadianPlan.QuotaAmount > 0 {
		// Process as topup - add quota directly
		err = model.IncreaseUserQuota(userId, int(aifadianPlan.QuotaAmount), true)
		if err != nil {
			processOk = false
			processMsg = fmt.Sprintf("增加额度失败: %s", err.Error())
			logger.LogError(c.Request.Context(), fmt.Sprintf("爱发电 webhook 增加额度失败 order_id=%s user_id=%d plan_id=%s quota=%d error=%s", payload.OrderID, userId, payload.PlanID, aifadianPlan.QuotaAmount, err.Error()))
		} else {
			processOk = true
			processMsg = fmt.Sprintf("充值成功，增加额度: %d", aifadianPlan.QuotaAmount)
			model.RecordTopupLog(userId, fmt.Sprintf("爱发电充值成功，订单: %s，充值金额: %s，获取额度: %d", payload.OrderID, payload.TotalAmount, aifadianPlan.QuotaAmount), c.ClientIP(), "aifadian", "aifadian")
			logger.LogInfo(c.Request.Context(), fmt.Sprintf("爱发电 webhook 充值成功 order_id=%s user_id=%d plan_id=%s quota=%d", payload.OrderID, userId, payload.PlanID, aifadianPlan.QuotaAmount))
		}
	} else {
		processMsg = fmt.Sprintf("plan_type=%s 配置无效", aifadianPlan.PlanType)
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("爱发电 webhook 配置无效 order_id=%s plan_id=%s plan_type=%s", payload.OrderID, payload.PlanID, aifadianPlan.PlanType))
	}

	// Save the order
	createdOrder = &model.AifadianOrder{
		OrderId:     payload.OrderID,
		UserId:      userId,
		PlanId:      payload.PlanID,
		Remark:      payload.Remark,
		TotalAmount: payload.TotalAmount,
		OrderStatus: payload.Status,
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

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success"})
}

// processAifadianSubscriptionOrder creates a subscription order and completes it.
func processAifadianSubscriptionOrder(userId int, aifadianPlan *model.AifadianPlan, payload AifadianCallbackPayload) (bool, string) {
	// Get the subscription plan
	subPlan, err := model.GetSubscriptionPlanById(aifadianPlan.SubscriptionPlanId)
	if err != nil {
		return false, fmt.Sprintf("未找到订阅套餐 ID %d", aifadianPlan.SubscriptionPlanId)
	}

	// Create trade no based on aifadian order_id
	tradeNo := "AFD" + payload.OrderID
	if len(tradeNo) > 255 {
		tradeNo = tradeNo[:255]
	}

	// Parse amount
	amount := 0.0
	if f, err := strconv.ParseFloat(payload.TotalAmount, 64); err == nil {
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
	if err := model.CompleteSubscriptionOrder(tradeNo, common.GetJsonString(payload), model.PaymentProviderEpay, "aifadian"); err != nil {
		return false, fmt.Sprintf("完成订阅订单失败: %s", err.Error())
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

	monthStr := c.DefaultQuery("month", "1")
	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 {
		month = 1
	}

	// Build remark: "用户名:wxkj;用户ID:1。请勿修改或者删除这里的信息以防充值不到账"
	remark := fmt.Sprintf("用户名:%s;用户ID:%d。请勿修改或者删除这里的信息以防充值不到账", user.Username, userId)

	// Build the Aifadian payment URL
	v := url.Values{}
	v.Set("plan_id", planId)
	v.Set("product_type", "0")
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
// GET /api/admin/aifadian/plans
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
	PlanId            string `json:"plan_id"`
	Name              string `json:"name"`
	PlanType          string `json:"plan_type"` // subscription or topup
	SubscriptionPlanId int   `json:"subscription_plan_id"`
	QuotaAmount       int64  `json:"quota_amount"`
	Enabled           bool   `json:"enabled"`
}

// AdminCreateAifadianPlan creates a new Aifadian plan mapping.
// POST /api/admin/aifadian/plans
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
// PUT /api/admin/aifadian/plans/:id
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

	existing.PlanId = req.PlanId
	existing.Name = req.Name
	existing.PlanType = req.PlanType
	existing.SubscriptionPlanId = req.SubscriptionPlanId
	existing.QuotaAmount = req.QuotaAmount
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
// DELETE /api/admin/aifadian/plans/:id
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
// GET /api/admin/aifadian/orders
func AdminGetAifadianOrders(c *gin.Context) {
	// For simplicity, return orders from the model package
	// We'll add a proper query later
	type orderRow struct {
		Order model.AifadianOrder `json:"order"`
	}
	_ = orderRow{}
	common.ApiErrorMsg(c, "暂未实现完整列表查询")
}
