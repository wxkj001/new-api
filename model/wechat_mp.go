package model

import (
	"time"
)

// WeChatMpLoginCode stores a login session for WeChat Mini Program scan-to-login.
// The QR code image is cached so it can be reused across sessions (rotation).
// When a session completes, the Scene+QrImage can be recycled for a new session.
type WeChatMpLoginCode struct {
	Id        int       `json:"id" gorm:"primaryKey;autoIncrement"`
	Code      string    `json:"code" gorm:"type:varchar(64);uniqueIndex;not null"`   // UUID for frontend polling
	Scene     string    `json:"scene" gorm:"type:varchar(32);uniqueIndex;not null"`  // scene value embedded in QR code
	QrImage   []byte    `json:"-" gorm:"type:bytea"`                             // cached QR code image bytes (PNG)
	Status    string    `json:"status" gorm:"type:varchar(20);default:pending;not null"` // pending, success, failed, expired
	UserId    int       `json:"user_id" gorm:"type:int;default:0"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	ExpiresAt time.Time `json:"expires_at" gorm:"index"`
}

const (
	WeChatMpCodeStatusPending = "pending"
	WeChatMpCodeStatusSuccess = "success"
	WeChatMpCodeStatusFailed  = "failed"
	WeChatMpCodeStatusExpired = "expired"
)

// CreateWeChatMpLoginCode inserts a new login code entry
func CreateWeChatMpLoginCode(code, scene string, qrImage []byte, ttl time.Duration) error {
	now := time.Now()
	entry := WeChatMpLoginCode{
		Code:      code,
		Scene:     scene,
		QrImage:   qrImage,
		Status:    WeChatMpCodeStatusPending,
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}
	return DB.Create(&entry).Error
}

// ReuseWeChatMpLoginCode recycles an existing entry: resets it with a new polling code,
// clears the user ID, and sets status back to pending. The Scene and QrImage are preserved
// so the QR code doesn't need to be regenerated (rotation/reuse).
func ReuseWeChatMpLoginCode(id int, code string, ttl time.Duration) error {
	now := time.Now()
	return DB.Model(&WeChatMpLoginCode{}).Where("id = ?", id).Updates(map[string]interface{}{
		"code":      code,
		"status":    WeChatMpCodeStatusPending,
		"user_id":   0,
		"expires_at": now.Add(ttl),
		"created_at": now,
	}).Error
}

// GetWeChatMpLoginCodeByCode retrieves a login code entry by the polling code
func GetWeChatMpLoginCodeByCode(code string) (*WeChatMpLoginCode, error) {
	var entry WeChatMpLoginCode
	err := DB.Where("code = ?", code).First(&entry).Error
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// GetWeChatMpLoginCodeByScene retrieves a login code entry by the scene value
func GetWeChatMpLoginCodeByScene(scene string) (*WeChatMpLoginCode, error) {
	var entry WeChatMpLoginCode
	err := DB.Where("scene = ? AND status = ?", scene, WeChatMpCodeStatusPending).First(&entry).Error
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// GetReusableWeChatMpScene retrieves a previously used scene+QR image that can be recycled.
// Looks for a non-active entry with a cached QR image. An expired pending record
// (user closed the page without scanning) is also eligible for reuse.
func GetReusableWeChatMpScene() (*WeChatMpLoginCode, error) {
	var entry WeChatMpLoginCode
	err := DB.Where("qr_image IS NOT NULL AND (status != ? OR expires_at < ?)",
		WeChatMpCodeStatusPending, time.Now()).
		Order("id DESC").First(&entry).Error
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// UpdateWeChatMpLoginCodeSuccess marks the code as successful and sets the user ID
func UpdateWeChatMpLoginCodeSuccess(code string, userId int) error {
	return DB.Model(&WeChatMpLoginCode{}).Where("code = ?", code).Updates(map[string]interface{}{
		"status":  WeChatMpCodeStatusSuccess,
		"user_id": userId,
	}).Error
}

// UpdateWeChatMpLoginCodeFailed marks the code as failed
func UpdateWeChatMpLoginCodeFailed(code string) error {
	return DB.Model(&WeChatMpLoginCode{}).Where("code = ?", code).Update("status", WeChatMpCodeStatusFailed).Error
}

// CleanExpiredWeChatMpLoginCodes removes expired entries (but keeps the most recent one for QR image reuse)
func CleanExpiredWeChatMpLoginCodes() error {
	return DB.Where("expires_at < ? AND status = ?", time.Now(), WeChatMpCodeStatusPending).
		Update("status", WeChatMpCodeStatusExpired).Error
}

// ForceReuseWeChatMpQrCode recycles an ended or expired entry when the QR code pool is full.
// Only picks non-pending or expired entries — never steals an active one.
func ForceReuseWeChatMpQrCode(code string, ttl time.Duration) (scene string, qrImage []byte, err error) {
	var entry WeChatMpLoginCode
	err = DB.Where("qr_image IS NOT NULL AND (status != ? OR expires_at < ?)",
		WeChatMpCodeStatusPending, time.Now()).
		Order("created_at ASC").First(&entry).Error
	if err != nil {
		return "", nil, err
	}
	if err = ReuseWeChatMpLoginCode(entry.Id, code, ttl); err != nil {
		return "", nil, err
	}
	return entry.Scene, entry.QrImage, nil
}

// CountWeChatMpQrCodes returns the number of entries that have a cached QR image
func CountWeChatMpQrCodes() int64 {
	var count int64
	DB.Model(&WeChatMpLoginCode{}).Where("qr_image IS NOT NULL").Count(&count)
	return count
}
