package platform

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
)

var errPWAPushDNS = errors.New("push DNS unavailable")

func migratePWAPushDiagnostics(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = ?", 29).Count(&count).Error; err != nil || count != 0 {
		return err
	}
	// Add only diagnostic columns; preserve existing subscriptions and receipts.
	for _, field := range []string{"Provider", "ProviderStatus", "ErrorCode", "LastAttemptAt"} {
		if !tx.Migrator().HasColumn(&PWAPushDelivery{}, field) {
			if err := tx.Migrator().AddColumn(&PWAPushDelivery{}, field); err != nil {
				return err
			}
		}
	}
	return tx.Create(&Migration{Version: 29, AppliedAt: time.Now().UTC()}).Error
}

func pwaPushProviderName(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || !pwaPushProvider(endpoint) {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case strings.HasSuffix(host, ".push.apple.com"):
		return "apple"
	case host == "fcm.googleapis.com" || host == "android.googleapis.com":
		return "google"
	case host == "push.services.mozilla.com" || strings.HasSuffix(host, ".push.services.mozilla.com"):
		return "mozilla"
	case strings.HasSuffix(host, ".notify.windows.com"):
		return "microsoft"
	}
	return ""
}

// Only fixed diagnostic codes leave the sender. A transport error or provider
// response may contain a private subscription endpoint, so never return it raw.
func pwaPushNetworkCode(err error) string {
	var dns *net.DNSError
	var unknown x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "network_timeout"
	case errors.As(err, &dns), errors.Is(err, errPWAPushDNS):
		return "dns_failed"
	case errors.As(err, &unknown), errors.As(err, &invalid), errors.As(err, &hostname):
		return "tls_failed"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "network_timeout"
	}
	return "network_failed"
}

type pwaPushDeliveryView struct {
	PWAPushDelivery
	Stage         string     `json:"stage"`
	NextAttemptAt *time.Time `json:"next_attempt_at"`
}

func newPWAPushDeliveryView(d PWAPushDelivery) pwaPushDeliveryView {
	// Old rows may predate structured diagnostics; do not expose legacy free text.
	d.LastError = ""
	stage := d.Status
	switch {
	case d.ClickedAt != nil:
		stage = "clicked"
	case d.DisplayedAt != nil:
		stage = "displayed"
	case d.SentAt != nil || d.Status == "sent":
		stage = "accepted"
	}
	v := pwaPushDeliveryView{PWAPushDelivery: d, Stage: stage}
	if d.Status == "pending" && !d.NextAttemptAt.IsZero() {
		v.NextAttemptAt = &d.NextAttemptAt
	}
	return v
}
