package model

import "time"

// WeChatMpReferralCode stores a permanent referral QR code for a user.
// Each user gets one referral code with a cached QR image.
// The scene value is embedded in the QR code. When someone scans it and
// logs in via the mini program, the backend records the referral relationship.
type WeChatMpReferralCode struct {
	Id        int       `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId    int       `json:"user_id" gorm:"uniqueIndex;not null"`
	Scene     string    `json:"scene" gorm:"type:varchar(32);uniqueIndex;not null"`
	QrImage   []byte    `json:"-" gorm:"type:bytea"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
}

func (WeChatMpReferralCode) TableName() string {
	return "wechat_mp_referral_codes"
}

// GetWeChatMpReferralCodeByUserId returns the user's referral code record
func GetWeChatMpReferralCodeByUserId(userId int) (*WeChatMpReferralCode, error) {
	var code WeChatMpReferralCode
	err := DB.Where("user_id = ?", userId).First(&code).Error
	if err != nil {
		return nil, err
	}
	return &code, nil
}

// CreateWeChatMpReferralCode inserts a new referral code record
func CreateWeChatMpReferralCode(userId int, scene string, qrImage []byte) error {
	entry := WeChatMpReferralCode{
		UserId:  userId,
		Scene:   scene,
		QrImage: qrImage,
	}
	return DB.Create(&entry).Error
}
