package platform

import (
	"gorm.io/gorm"
	"time"
)

// All money uses integer millionths of a US dollar. No floating-point settlement.
const Dollar int64 = 1_000_000

type User struct {
	ID                string     `json:"id" gorm:"primaryKey;size:32"`
	Email             string     `json:"email" gorm:"uniqueIndex;size:254;not null"`
	Name              string     `json:"name" gorm:"size:80"`
	PasswordHash      string     `json:"-"`
	Role              string     `json:"role" gorm:"size:16;not null"`
	Disabled          bool       `json:"disabled"`
	BalanceMicros     int64      `json:"balance_micros" gorm:"not null;check:balance_micros >= 0"`
	HeldMicros        int64      `json:"held_micros" gorm:"not null;check:held_micros >= 0 AND held_micros <= balance_micros"`
	GiftBalanceMicros int64      `json:"gift_balance_micros" gorm:"not null;default:0;check:gift_balance_micros >= 0"`
	GiftHeldMicros    int64      `json:"gift_held_micros" gorm:"not null;default:0;check:gift_held_micros >= 0 AND gift_held_micros <= gift_balance_micros"`
	ProUntil          *time.Time `json:"pro_until"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	// Keep the tombstone available to settlement/history queries. Interactive
	// lists and authentication explicitly exclude deleted accounts.
	DeletedAt *time.Time `json:"-" gorm:"index"`
}

func (u User) PlanID(now time.Time) string {
	if u.ProUntil != nil && u.ProUntil.After(now) {
		return "pro"
	}
	return "free"
}

type Plan struct {
	ID            string `json:"id" gorm:"primaryKey;size:16"`
	Name          string `json:"name"`
	PriceMicros   int64  `json:"price_micros" gorm:"check:price_micros >= 0"`
	AppLimit      int64  `json:"app_limit"`
	DownloadLimit int64  `json:"download_limit"`
	StorageBytes  int64  `json:"storage_bytes"`
}
type RechargeTier struct {
	ThresholdMicros int64 `json:"threshold_micros"`
	Percent         int64 `json:"percent"`
}
type DownloadDomainRule struct {
	Usage      string `json:"usage,omitempty"`
	URL        string `json:"url"`
	Audience   string `json:"audience"`
	Remark     string `json:"remark"`
	LegacyOnly bool   `json:"legacy_only,omitempty"`
}

func defaultRechargeTiers() []RechargeTier {
	return []RechargeTier{{200 * Dollar, 15}, {500 * Dollar, 35}, {1000 * Dollar, 50}}
}

type Settings struct {
	ID                     uint                 `json:"id" gorm:"primaryKey"`
	SiteName               string               `json:"site_name"`
	DownloadBaseURL        string               `json:"download_base_url"`
	DownloadDomainMode     string               `json:"download_domain_mode" gorm:"not null;default:unified"`
	FreeDownloadBaseURL    string               `json:"free_download_base_url"`
	PaidDownloadBaseURL    string               `json:"paid_download_base_url"`
	DownloadDomainRules    []DownloadDomainRule `json:"download_domain_rules" gorm:"serializer:json;type:text"`
	RegistrationOpen       bool                 `json:"registration_open"`
	DownloadPriceMicros    int64                `json:"download_price_micros"`
	MeteredBilling         bool                 `json:"metered_billing"`
	PWAInstallPriceMicros  int64                `json:"pwa_install_price_micros"`
	CloakUVPriceMicros     int64                `json:"cloak_uv_price_micros"`
	ReturnFlowPriceMicros  int64                `json:"return_flow_price_micros"`
	PWAPushPriceMicros     int64                `json:"pwa_push_price_micros"`
	RegistrationGiftLimit  int64                `json:"registration_gift_limit"`
	RegistrationGiftMicros int64                `json:"registration_gift_micros"`
	AppLimit               int64                `json:"app_limit"`
	StorageLimitBytes      int64                `json:"storage_limit_bytes"`
	MaxUploadBytes         int64                `json:"max_upload_bytes"`
	DomainLimit            int64                `json:"domain_limit"`
	USDTRateMicros         int64                `json:"usdt_rate_micros"`
	AlipayCNYRateMicros    int64                `json:"alipay_cny_rate_micros" gorm:"not null;default:6650000"`
	USDTWallet             *string              `json:"usdt_wallet"`
	SupportEmail           string               `json:"support_email"`
	SupportURL             string               `json:"support_url" gorm:"not null;default:''"`
	RechargeBonusEnabled   bool                 `json:"recharge_bonus_enabled" gorm:"not null;default:true"`
	RechargeBonusTiers     []RechargeTier       `json:"recharge_bonus_tiers" gorm:"serializer:json;type:text"`
	UpdatedAt              time.Time            `json:"updated_at"`
}
type Session struct {
	Hash      string `gorm:"primaryKey;size:64"`
	UserID    string `gorm:"index;size:32"`
	CSRF      string
	ExpiresAt time.Time `gorm:"index"`
}
type Challenge struct {
	ID        string `gorm:"primaryKey;size:32"`
	Hash      string
	ExpiresAt time.Time `gorm:"index"`
}
type RateBucket struct {
	Key       string `gorm:"primaryKey;size:64"`
	Count     int64
	ExpiresAt time.Time `gorm:"index"`
}
type Application struct {
	LandingSource   string               `json:"landing_source,omitempty" gorm:"-"`
	LandingTemplate string               `json:"landing_template,omitempty" gorm:"-"`
	DIYLanding      *DIYLandingReference `json:"diy_landing,omitempty" gorm:"-"`
	ID              string               `json:"id" gorm:"primaryKey;size:32"`
	UserID          string               `json:"user_id" gorm:"index;size:32;not null"`
	Slug            string               `json:"slug" gorm:"uniqueIndex;size:32;not null"`
	Name            string               `json:"name" gorm:"size:100"`
	Description     string               `json:"description"`
	Version         string               `json:"version" gorm:"size:40"`
	Platform        string               `json:"platform" gorm:"size:16"`
	FileKey         string               `json:"-" gorm:"size:32"`
	FileName        string               `json:"file_name"`
	Size            int64                `json:"size"`
	SHA256          string               `json:"sha256"`
	Enabled         bool                 `json:"enabled"`
	DownloadMode    string               `json:"download_mode" gorm:"not null;default:direct"`
	Blocked         bool                 `json:"blocked"`
	Downloads       int64                `json:"downloads"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
	DeletedAt       gorm.DeletedAt       `json:"-" gorm:"index"`
}
type Domain struct {
	VerificationMethod      string     `json:"verification_method"`
	VerificationRequestedAt *time.Time `json:"-"`
	SSLStatus               string     `json:"ssl_status" gorm:"not null;default:pending"`
	SSLError                string     `json:"ssl_error"`
	SSLRetryAt              *time.Time `json:"-"`
	Remark                  string     `json:"remark" gorm:"size:120"`
	ID                      string     `json:"id" gorm:"primaryKey;size:32"`
	UserID                  string     `json:"user_id" gorm:"index;size:32"`
	Hostname                string     `json:"hostname" gorm:"uniqueIndex;size:253"`
	Token                   string     `json:"token"`
	VerifiedAt              *time.Time `json:"verified_at"`
	LastError               string     `json:"last_error"`
	CreatedAt               time.Time  `json:"created_at"`
}
type Usage struct {
	UserID       string `json:"user_id" gorm:"primaryKey;size:32"`
	Period       string `json:"period" gorm:"primaryKey;size:7"`
	FreeReserved int64  `json:"free_reserved"`
	FreeUsed     int64  `json:"free_used"`
	PaidUsed     int64  `json:"paid_used"`
	SpentMicros  int64  `json:"spent_micros"`
}

// Gift counters belong to a registration/paid period, not a calendar Usage row.
// Keep old grants so in-flight downloads can settle after a period changes.
type DownloadGrant struct {
	ID        string `gorm:"primaryKey;size:64"`
	UserID    string `gorm:"index:idx_grant_owner_period,priority:1;size:32"`
	PlanID    string `gorm:"size:16"`
	Limit     int64
	Used      int64
	Reserved  int64
	StartsAt  time.Time `gorm:"index:idx_grant_owner_period,priority:2"`
	ExpiresAt time.Time `gorm:"index"`
}
type Ticket struct {
	Metered       bool
	Hash          string `gorm:"primaryKey;size:64"`
	AppID         string `gorm:"index;size:32"`
	UserID        string `gorm:"size:32"`
	FileKey       string `gorm:"size:32"`
	VisitorHash   string `gorm:"size:64"`
	Host          string
	Charged       bool
	Reserved      bool `gorm:"index"`
	Released      bool
	Free          bool
	PriceMicros   int64
	Period        string `gorm:"size:7"`
	GrantID       string `gorm:"size:64;index"`
	Size          int64
	AppName       string
	Ranges        string
	IP            string `gorm:"size:64"`
	UserAgent     string
	Source        string
	Device        string
	OS            string
	Browser       string
	AttributionID string `gorm:"size:32;index"`
	StartedAt     *time.Time
	ExpiresAt     time.Time `gorm:"index"`
}
type Download struct {
	Metered       bool      `json:"metered"`
	ID            string    `json:"id" gorm:"primaryKey;size:32"`
	TicketHash    string    `json:"-" gorm:"uniqueIndex;size:64"`
	UserID        string    `json:"user_id" gorm:"index:idx_download_owner_time,priority:1;size:32"`
	AppID         string    `json:"app_id" gorm:"index;size:32"`
	AppName       string    `json:"app_name"`
	AmountMicros  int64     `json:"amount_micros"`
	IP            string    `json:"ip" gorm:"size:64"`
	UserAgent     string    `json:"user_agent"`
	UABlocked     bool      `json:"ua_blocked" gorm:"-"`
	Source        string    `json:"source"`
	Device        string    `json:"device"`
	OS            string    `json:"os"`
	Browser       string    `json:"browser"`
	Bytes         int64     `json:"bytes"`
	AttributionID string    `json:"attribution_id" gorm:"size:32;index"`
	StartedAt     time.Time `json:"started_at"`
	CompletedAt   time.Time `json:"completed_at"`
	CreatedAt     time.Time `json:"created_at" gorm:"index:idx_download_owner_time,priority:2;index"`
}
type Ledger struct {
	BalanceType   string    `json:"balance_type" gorm:"not null;default:balance;size:16"`
	ID            string    `json:"id" gorm:"primaryKey;size:32"`
	UserID        string    `json:"user_id" gorm:"index;size:32"`
	Reference     string    `json:"reference" gorm:"uniqueIndex;size:128"`
	Kind          string    `json:"kind"`
	AmountMicros  int64     `json:"amount_micros"`
	BalanceMicros int64     `json:"balance_micros"`
	Note          string    `json:"note"`
	CreatedAt     time.Time `json:"created_at" gorm:"index"`
}
type Order struct {
	ID               string     `json:"id" gorm:"primaryKey;size:32"`
	UserID           string     `json:"user_id" gorm:"index;size:32"`
	IdempotencyKey   string     `json:"-" gorm:"uniqueIndex;size:128"`
	Method           string     `json:"method"`
	AmountMicros     int64      `json:"amount_micros"`
	BonusMicros      int64      `json:"bonus_micros" gorm:"not null;default:0;check:bonus_micros >= 0"`
	Status           string     `json:"status" gorm:"index;size:20"`
	TradeID          *string    `json:"trade_id" gorm:"uniqueIndex;size:128"`
	PayURL           string     `json:"pay_url"`
	PaymentCurrency  string     `json:"payment_currency" gorm:"size:3;not null;default:''"`
	AlipayRateMicros int64      `json:"alipay_rate_micros" gorm:"not null;default:0"`
	FeeMicros        int64      `json:"fee_micros" gorm:"not null;default:0"`
	PayCNYCents      int64      `json:"pay_cny_cents" gorm:"not null;default:0"`
	USDTUnits        int64      `json:"usdt_units"`
	USDTProvider     string     `json:"usdt_provider" gorm:"not null;default:tron"`
	Wallet           string     `json:"wallet"`
	RateMicros       int64      `json:"rate_micros"`
	CreatedAt        time.Time  `json:"created_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
	PaidAt           *time.Time `json:"paid_at"`
}

// Never recycle a payment amount for a receiving wallet. A delayed transfer
// cannot accidentally fund a different order.
type USDTReservation struct {
	Wallet  string `gorm:"primaryKey;size:40"`
	Units   int64  `gorm:"primaryKey"`
	OrderID string `gorm:"uniqueIndex;size:32"`
}
type Audit struct {
	ID        string    `json:"id" gorm:"primaryKey;size:32"`
	ActorID   string    `json:"actor_id" gorm:"size:32"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at" gorm:"index"`
}
type Migration struct {
	Version   int `gorm:"primaryKey"`
	AppliedAt time.Time
}
