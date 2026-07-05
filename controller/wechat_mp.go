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

const weChatMpCodeTTL = 5 * time.Minute

var wechatMpCache = cache.NewMemory()

// getMiniProgram returns a WeChat Mini Program client using the configured credentials
func getMiniProgram() *miniprogram.MiniProgram {
	cfg := &miniConfig.Config{
		AppID:     common.WeChatMpAppId,
		AppSecret: common.WeChatMpAppSecret,
		Cache:     wechatMpCache,
	}
	return miniprogram.NewMiniProgram(cfg)
}

// WeChatMpGenerateURL generates a QR code for WeChat Mini Program login.
// POST /api/wechat-mp/url
func WeChatMpGenerateURL(c *gin.Context) {
	if !common.WeChatMpAuthEnabled {
		common.ApiErrorI18n(c, i18n.MsgWeChatMpLoginNotEnabled)
		return
	}

	// Generate unique polling code
	code := uuid.New().String()

	var scene string
	var qrImage []byte

	// Try to reuse a previously generated scene+QR code (rotation)
	reusable, err := model.GetReusableWeChatMpScene()
	if err == nil && reusable != nil && len(reusable.QrImage) > 0 {
		scene = reusable.Scene
		qrImage = reusable.QrImage
		logger.LogDebug(c.Request.Context(), fmt.Sprintf("[WeChatMp] GenerateURL: reusing scene=%s", scene))
		// Recycle the existing entry with a new polling code
		if err := model.ReuseWeChatMpLoginCode(reusable.Id, code, weChatMpCodeTTL); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("[WeChatMp] GenerateURL: reuse code error: %s", err.Error()))
			common.ApiErrorI18n(c, i18n.MsgWeChatMpLoginGenerateQRError)
			return
		}
	} else if common.WeChatMpMaxQrCodes > 0 && model.CountWeChatMpQrCodes() >= int64(common.WeChatMpMaxQrCodes) {
		// Pool full — only reuse ended/expired entries, never steal active ones
		var reuseErr error
		scene, qrImage, reuseErr = model.ForceReuseWeChatMpQrCode(code, weChatMpCodeTTL)
		if reuseErr != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("[WeChatMp] GenerateURL: pool full, no reusable entry available"))
			common.ApiErrorI18n(c, i18n.MsgWeChatMpLoginBusy)
			return
		}
	} else {
		// Generate a new scene value (short unique string)
		scene = generateScene()

		// Call GetWXACodeUnlimit to generate QR code
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

		// Store new login code entry
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

// WeChatMpLogin handles the login request from the mini program.
// The mini program calls wx.login() to get a js_code, then sends it here
// along with the scene value from the QR code.
// POST /api/wechat-mp/login
type WeChatMpLoginRequest struct {
	Scene string `json:"scene" binding:"required"`
	Code  string `json:"code" binding:"required"` // wx.login() js_code
}

func WeChatMpLogin(c *gin.Context) {
	var req WeChatMpLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Invalid request parameters",
		})
		return
	}

	// Validate scene code
	loginEntry, err := model.GetWeChatMpLoginCodeByScene(req.Scene)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("[WeChatMp] Login: scene not found or not pending: %s, err=%v", req.Scene, err))
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Invalid or expired login code",
		})
		return
	}

	if time.Now().After(loginEntry.ExpiresAt) {
		model.UpdateWeChatMpLoginCodeFailed(loginEntry.Code)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Login code expired",
		})
		return
	}

	// Exchange js_code for openid via Code2Session
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

	// Find or create user
	var user model.User
	if model.IsWeChatMpOpenIdAlreadyTaken(openId) {
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
		if !common.RegisterEnabled {
			model.UpdateWeChatMpLoginCodeFailed(loginEntry.Code)
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "Registration is disabled",
			})
			return
		}

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

		// Auto-generate an API access token for new users
		if err := generateUserAccessToken(&user); err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("[WeChatMp] Login: generate token error: %s", err.Error()))
			// Non-fatal: user can still regenerate manually
		}
	}

	// Mark login code as success
	if err := model.UpdateWeChatMpLoginCodeSuccess(loginEntry.Code, user.Id); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("[WeChatMp] Login: update code success error: %s", err.Error()))
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Login successful",
	})
}

// WeChatMpCheckStatus polls the login code status for the web frontend.
// GET /api/wechat-mp/status?code=xxx
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

		setupLogin(&user, c)

	case model.WeChatMpCodeStatusFailed, model.WeChatMpCodeStatusExpired:
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"status":  loginEntry.Status,
			"message": "Login failed or expired",
		})
	}
}

// generateScene creates a short unique scene value for the QR code.
// Max 32 chars, only supports: 0-9, a-z, A-Z, and special chars !#$&'()*+,/:;=?@-._~
func generateScene() string {
	return "s_" + uuid.New().String()[:8]
}

// generateUserAccessToken generates and sets a random API access token for the user.
// Uses the existing business logic (GenerateKey + Token model) and the user's own group.
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
		Group:          user.Group,
	}

	if err := token.Insert(); err != nil {
		return fmt.Errorf("insert token failed: %w", err)
	}

	return nil
}
