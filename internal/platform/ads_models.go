package platform

import "time"

type AdConfig struct {
	Disabled              bool      `json:"disabled" gorm:"not null;default:false"`
	Mode                  string    `json:"mode" gorm:"size:8;not null;default:deep"`
	AppID                 string    `json:"app_id" gorm:"primaryKey;size:40"`
	UserID                string    `json:"-" gorm:"index;size:32"`
	FacebookEnabled       bool      `json:"facebook_enabled"`
	FacebookPixel         string    `json:"facebook_pixel"`
	FacebookToken         string    `json:"-"`
	FacebookTestCode      string    `json:"facebook_test_code"`
	TikTokEnabled         bool      `json:"tiktok_enabled"`
	TikTokPixel           string    `json:"tiktok_pixel"`
	TikTokToken           string    `json:"-"`
	TikTokTestCode        string    `json:"tiktok_test_code"`
	OpenFacebookEvent     string    `json:"open_facebook_event"`
	RegisterFacebookEvent string    `json:"register_facebook_event"`
	DownloadFacebookEvent string    `json:"download_facebook_event"`
	OpenTikTokEvent       string    `json:"open_tiktok_event"`
	RegisterTikTokEvent   string    `json:"register_tiktok_event"`
	DownloadTikTokEvent   string    `json:"download_tiktok_event"`
	AllowedOrigin         string    `json:"allowed_origin"`
	DeepLink              string    `json:"deep_link"`
	AllowClientEvents     bool      `json:"allow_client_events"`
	APISecret             string    `json:"-"`
	UpdatedAt             time.Time `json:"updated_at"`
}
type Attribution struct {
	SourceAttributionID string `gorm:"size:32;index;not null;default:''"`
	ID                  string `gorm:"primaryKey;size:32"`
	AppID               string `gorm:"index;size:40"`
	VisitorHash         string
	FBClid              string
	FBC                 string
	FBP                 string
	TTClid              string
	TTP                 string
	IP                  string
	UserAgent           string
	Source              string
	PageURL             string
	Consent             bool
	CreatedAt           time.Time
	ExpiresAt           time.Time `gorm:"index"`
}
type Conversion struct {
	DownloadedAppID string     `json:"downloaded_app_id,omitempty" gorm:"size:32;index"`
	ID              string     `json:"id" gorm:"primaryKey;size:32"`
	AppID           string     `json:"app_id" gorm:"index;size:40"`
	UserID          string     `json:"-" gorm:"index;size:32"`
	AttributionID   string     `json:"attribution_id" gorm:"uniqueIndex:idx_conversion_once,priority:1;size:32"`
	Event           string     `json:"event" gorm:"uniqueIndex:idx_conversion_once,priority:2;size:30"`
	EventID         string     `json:"event_id" gorm:"uniqueIndex;size:64"`
	ExternalIDHash  string     `json:"-"`
	IP              string     `json:"ip"`
	UserAgent       string     `json:"user_agent"`
	SourceURL       string     `json:"source_url"`
	Consent         bool       `json:"consent"`
	VerifiedBy      string     `json:"verified_by"`
	EventTime       time.Time  `json:"event_time"`
	CreatedAt       time.Time  `json:"created_at"`
	Deliveries      []Delivery `json:"deliveries" gorm:"foreignKey:ConversionID"`
}
type Delivery struct {
	ID                string     `json:"id" gorm:"primaryKey;size:32"`
	ConversionID      string     `json:"conversion_id" gorm:"uniqueIndex:idx_delivery_once,priority:1;size:32"`
	Platform          string     `json:"platform" gorm:"uniqueIndex:idx_delivery_once,priority:2;size:16"`
	EventName         string     `json:"event_name"`
	PixelID           string     `json:"pixel_id"`
	Status            string     `json:"status" gorm:"index:idx_delivery_due,priority:1"`
	Attempts          int        `json:"attempts"`
	NextAttemptAt     time.Time  `json:"next_attempt_at" gorm:"index:idx_delivery_due,priority:2"`
	LeaseUntil        time.Time  `json:"-"`
	LastError         string     `json:"last_error"`
	ProviderRequestID string     `json:"provider_request_id"`
	CreatedAt         time.Time  `json:"created_at"`
	SentAt            *time.Time `json:"sent_at"`
}
