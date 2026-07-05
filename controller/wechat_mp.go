/*
微信小程序扫码登录控制器

流程概述：
1. Web 前端请求生成二维码 → GET /api/wechat-mp/url → 返回 polling code + base64 二维码
2. 用户用微信扫描二维码 → 打开小程序 → 小程序调用 wx.login() 获取 js_code
3. 小程序将 js_code + scene 发送到 → POST /api/wechat-mp/login
4. 后端通过 Code2Session 换取 openid → 查找或创建用户 → 新用户自动生成 API Key
5. Web 前端轮询 → GET /api/wechat-mp/status?code=xxx → 登录成功后自动跳转

依赖微信小程序库 github.com/silenceper/wechat/v2
配置项见 common 包中的 WeChatMpAppId / WeChatMpAppSecret 等参数
*/
package controller

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/silenceper/wechat/v2/cache"
	"github.com/silenceper/wechat/v2/miniprogram"
	miniConfig "github.com/silenceper/wechat/v2/miniprogram/config"
	"github.com/silenceper/wechat/v2/miniprogram/qrcode"
)

// weChatMpCodeTTL 登录 polling code 的有效期（5 分钟），超时后需要重新扫码
const weChatMpCodeTTL = 5 * time.Minute

// wechatMpCache 小程序 API 的本地缓存实例（用于 access_token 管理）
var wechatMpCache = cache.NewMemory()

// getMiniProgram 根据系统配置创建微信小程序客户端
// 从 common.WeChatMpAppId / WeChatMpAppSecret 读取凭证
func getMiniProgram() *miniprogram.MiniProgram {
	cfg := &miniConfig.Config{
		AppID:     common.WeChatMpAppId,
		AppSecret: common.WeChatMpAppSecret,
		Cache:     wechatMpCache,
	}
	return miniprogram.NewMiniProgram(cfg)
}

// WeChatMpGenerateURL 生成微信小程序登录二维码
// POST /api/wechat-mp/url
//
// 策略说明：
//  1. 优先复用已过期但未删除的二维码（减少微信 API 调用次数，避免达到二维码总数上限）
//  2. 若池子满了则强制回收最早的过期条目
//  3. 以上均不满足时才调用微信 GetWXACodeUnlimit 生成新码
//
// 返回数据：
//  - code:     polling 标识符（前端用此轮询登录状态）
//  - qr_image: base64 编码的二维码图片 data URI
func WeChatMpGenerateURL(c *gin.Context) {
	if !common.WeChatMpAuthEnabled {
		common.ApiErrorI18n(c, i18n.MsgWeChatMpLoginNotEnabled)
		return
	}

	// 生成唯一的 polling code，用于前端轮询
	code := uuid.New().String()

	var scene string
	var qrImage []byte

	// 尝试复用已结束的二维码条目（旋转策略）
	reusable, err := model.GetReusableWeChatMpScene()
	if err == nil && reusable != nil && len(reusable.QrImage) > 0 {
		scene = reusable.Scene
		qrImage = reusable.QrImage
		logger.LogDebug(c.Request.Context(), fmt.Sprintf("[WeChatMp] GenerateURL: reusing scene=%s", scene))
		// 给已有记录绑定新的 polling code
		if err := model.ReuseWeChatMpLoginCode(reusable.Id, code, weChatMpCodeTTL); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("[WeChatMp] GenerateURL: reuse code error: %s", err.Error()))
			common.ApiErrorI18n(c, i18n.MsgWeChatMpLoginGenerateQRError)
			return
		}
	} else if common.WeChatMpMaxQrCodes > 0 && model.CountWeChatMpQrCodes() >= int64(common.WeChatMpMaxQrCodes) {
		// 池子已满：强制回收最早过期的条目（不会偷取活跃中的条目）
		var reuseErr error
		scene, qrImage, reuseErr = model.ForceReuseWeChatMpQrCode(code, weChatMpCodeTTL)
		if reuseErr != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("[WeChatMp] GenerateURL: pool full, no reusable entry available"))
			common.ApiErrorI18n(c, i18n.MsgWeChatMpLoginBusy)
			return
		}
	} else {
		// 生成新的 scene 值（短唯一字符串，最大 32 字符）
		scene = generateScene()

		// 调用微信 GetWXACodeUnlimit 生成小程序码
		mp := getMiniProgram()
		qrImage, err = mp.GetQRCode().GetWXACodeUnlimit(qrcode.QRCoder{
			Scene: scene,
			Page:  common.WeChatMpPagePath,
			Width: 430,
		})
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("[WeChatMp] GenerateURL: GetWXACodeUnlimit error: %s", err.Error()))
			common.ApiErrorI18n(c, i18n.MsgWeChatMpLoginGenerateQRError)
			return
		}
		logger.LogDebug(c.Request.Context(), fmt.Sprintf("[WeChatMp] GenerateURL: generated new scene=%s, qr_size=%d", scene, len(qrImage)))

		// 持久化新的登录码记录
		if err := model.CreateWeChatMpLoginCode(code, scene, qrImage, weChatMpCodeTTL); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("[WeChatMp] GenerateURL: create code error: %s", err.Error()))
			common.ApiErrorI18n(c, i18n.MsgWeChatMpLoginGenerateQRError)
			return
		}
	}

	qrBase64 := base64.StdEncoding.EncodeToString(qrImage)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"code":     code,
			"qr_image": "data:image/png;base64," + qrBase64,
		},
	})
}

// WeChatMpLoginRequest 小程序登录请求体
type WeChatMpLoginRequest struct {
	// Scene 二维码中的 scene 值，用于关联本次登录与 polling 记录
	Scene string `json:"scene" binding:"required"`
	// Code 小程序 wx.login() 返回的 js_code，用于换取 openid
	Code string `json:"code" binding:"required"`
}

// WeChatMpLogin 处理小程序发起的登录请求
// POST /api/wechat-mp/login
//
// 流程：
//  1. 校验 scene 对应的 polling 记录是否有效且未过期
//  2. 调用微信 Code2Session 接口，用 js_code 换取 openid
//  3. 已存在用户 → 直接登录
//  4. 新用户 → 自动注册并生成 API Key（Token），分组继承用户默认分组
//  5. 更新 polling 记录状态为成功
func WeChatMpLogin(c *gin.Context) {
	var req WeChatMpLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Invalid request parameters",
		})
		return
	}

	// 校验 scene 对应的 polling 记录
	loginEntry, err := model.GetWeChatMpLoginCodeByScene(req.Scene)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("[WeChatMp] Login: scene not found or not pending: %s, err=%v", req.Scene, err))
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Invalid or expired login code",
		})
		return
	}

	// 检查是否过期
	if time.Now().After(loginEntry.ExpiresAt) {
		model.UpdateWeChatMpLoginCodeFailed(loginEntry.Code)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Login code expired",
		})
		return
	}

	// 调用微信 Code2Session 接口，用 js_code 换取 openid 和 session_key
	mp := getMiniProgram()
	session, err := mp.GetAuth().Code2Session(req.Code)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("[WeChatMp] Login: Code2Session error: %s", err.Error()))
		model.UpdateWeChatMpLoginCodeFailed(loginEntry.Code)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "WeChat authorization failed",
		})
		return
	}

	openId := session.OpenID
	if openId == "" {
		logger.LogError(c.Request.Context(), "[WeChatMp] Login: empty openid from Code2Session")
		model.UpdateWeChatMpLoginCodeFailed(loginEntry.Code)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Failed to get WeChat user info",
		})
		return
	}

	logger.LogDebug(c.Request.Context(), fmt.Sprintf("[WeChatMp] Login: openid=%s, scene=%s", openId, req.Scene))

	// 根据 openid 查找或创建用户
	var user model.User
	if model.IsWeChatMpOpenIdAlreadyTaken(openId) {
		// 已有用户：填充用户信息
		user.WeChatMpOpenId = openId
		if err := user.FillUserByWeChatMpOpenId(); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("[WeChatMp] Login: fill user error: %s", err.Error()))
			model.UpdateWeChatMpLoginCodeFailed(loginEntry.Code)
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "Failed to find user",
			})
			return
		}
		if user.Id == 0 {
			model.UpdateWeChatMpLoginCodeFailed(loginEntry.Code)
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "User has been deleted",
			})
			return
		}
	} else {
		// 新用户：检查注册是否开启
		if !common.RegisterEnabled {
			model.UpdateWeChatMpLoginCodeFailed(loginEntry.Code)
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "Registration is disabled",
			})
			return
		}

		// 创建新用户
		user.Username = "wxmp_" + strconv.Itoa(model.GetMaxUserId()+1)
		user.DisplayName = "WeChat MP User"
		user.WeChatMpOpenId = openId
		user.Role = common.RoleCommonUser
		user.Status = common.UserStatusEnabled

		if err := user.Insert(0); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("[WeChatMp] Login: create user error: %s", err.Error()))
			model.UpdateWeChatMpLoginCodeFailed(loginEntry.Code)
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "Failed to create user",
			})
			return
		}

		// 为新用户自动生成 API Key（Token），方便扫码后直接使用
		if err := generateUserAccessToken(&user); err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("[WeChatMp] Login: generate token error: %s", err.Error()))
			// 生成失败不阻塞登录，用户可后续在控制台手动生成
		}
	}

	// 标记 polling 记录为登录成功
	if err := model.UpdateWeChatMpLoginCodeSuccess(loginEntry.Code, user.Id); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("[WeChatMp] Login: update code success error: %s", err.Error()))
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Login successful",
	})
}

// WeChatMpCheckStatus 供 Web 前端轮询登录状态
// GET /api/wechat-mp/status?code=xxx
//
// 状态机：
//  pending → 等待用户扫码
//  success → 登录成功，自动调用 setupLogin 设置会话
//  failed  → 登录失败
//  expired → 二维码过期
func WeChatMpCheckStatus(c *gin.Context) {
	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "code is required",
		})
		return
	}

	loginEntry, err := model.GetWeChatMpLoginCodeByCode(code)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"status":  model.WeChatMpCodeStatusExpired,
			"message": "Login code not found or expired",
		})
		return
	}

	switch loginEntry.Status {
	case model.WeChatMpCodeStatusPending:
		// 待扫码：检查是否已超时
		if time.Now().After(loginEntry.ExpiresAt) {
			model.UpdateWeChatMpLoginCodeFailed(code)
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"status":  model.WeChatMpCodeStatusExpired,
				"message": "Login code expired",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"status":  model.WeChatMpCodeStatusPending,
			"message": "Waiting for scan",
		})

	case model.WeChatMpCodeStatusSuccess:
		// 登录成功：获取用户信息并设置会话
		user := model.User{Id: loginEntry.UserId}
		if err := user.FillUserById(); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"status":  model.WeChatMpCodeStatusFailed,
				"message": "Failed to get user info",
			})
			return
		}

		if user.Status != common.UserStatusEnabled {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"status":  model.WeChatMpCodeStatusFailed,
				"message": "User is banned",
			})
			return
		}

		// 设置会话和 cookie，返回用户信息
		setupLogin(&user, c)

	case model.WeChatMpCodeStatusFailed, model.WeChatMpCodeStatusExpired:
		// 登录失败或已过期
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"status":  loginEntry.Status,
			"message": "Login failed or expired",
		})
	}
}

// generateScene 生成短唯一 scene 值
// 约束：最大 32 字符，仅支持 0-9 a-z A-Z 及 !#$&'()*+,/:;=?@-._~
func generateScene() string {
	return "s_" + uuid.New().String()[:8]
}

// generateUserAccessToken 为新注册用户自动创建 API Key
//
// 使用现有的业务逻辑：
//   - common.GenerateKey() 生成标准 48 位随机 key
//   - model.Token 模型持久化，分组留空由后端自动分配
//   - 无限额度、永久有效、启用状态
//
// 若生成失败仅记警告，不阻塞登录流程，用户可稍后在控制台手动生成
func generateUserAccessToken(user *model.User) error {
	key, err := common.GenerateKey()
	if err != nil {
		return fmt.Errorf("generate key failed: %w", err)
	}

	token := model.Token{
		UserId:         user.Id,
		Name:           "WeChat MP Auto",
		Key:            key,
		CreatedTime:    common.GetTimestamp(),
		AccessedTime:   common.GetTimestamp(),
		ExpiredTime:    -1,
		RemainQuota:    0,
		UnlimitedQuota: true,
		Status:         1,
		Group:          "",
	}

	if err := token.Insert(); err != nil {
		return fmt.Errorf("insert token failed: %w", err)
	}

	return nil
}
