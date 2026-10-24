package platform

import (
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"gorm.io/gorm"
)

const alipayFeePercent int64 = 5

func validAlipayRate(rate int64) bool { return rate >= Dollar/100 && rate <= 1000*Dollar }

// Amount/fee are USD-account millionths; rate is CNY per U in millionths.
// Round the final CNY price up to a cent once, never through floating point.
func alipayQuote(amount, rate int64) (fee, cents int64, err error) {
	if amount < Dollar || amount > 10000*Dollar || amount%10000 != 0 || !validAlipayRate(rate) {
		return 0, 0, problem(400, "充值金额或支付宝汇率无效")
	}
	fee = amount * alipayFeePercent / 100
	value := new(big.Int).Mul(big.NewInt(amount+fee), big.NewInt(rate))
	denominator := Dollar * 10000 // millionths squared -> CNY cents
	value.Add(value, big.NewInt(denominator-1)).Quo(value, big.NewInt(denominator))
	if !value.IsInt64() || value.Sign() <= 0 {
		return 0, 0, problem(400, "支付宝应付金额超出范围")
	}
	return fee, value.Int64(), nil
}

func formatCNYCents(cents int64) string { return fmt.Sprintf("%d.%02d", cents/100, cents%100) }

func migrateAlipayPayment(tx *gorm.DB) error {
	var n int64
	if err := tx.Model(&Migration{}).Where("version = 10").Count(&n).Error; err != nil || n > 0 {
		return err
	}
	// Existing orders keep an empty currency and their original settlement
	// semantics. Do not rewrite principal, gifts, receiving wallets or balances.
	if err := tx.AutoMigrate(&Order{}, &Settings{}); err != nil {
		return err
	}
	return tx.Create(&Migration{Version: 10, AppliedAt: time.Now().UTC()}).Error
}

var paymentReturnID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (s *Server) futoonReturn(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query["out_trade_no"]) > 1 {
		http.Error(w, "invalid order", http.StatusBadRequest)
		return
	}
	order := query.Get("out_trade_no")
	if order != "" && !paymentReturnID.MatchString(order) {
		http.Error(w, "invalid order", http.StatusBadRequest)
		return
	}
	location := s.Config.PublicURL + "/#/billing"
	if order != "" {
		location += "?order=" + order
	}
	// A browser return is not proof of payment. Only the signed asynchronous
	// notification can settle; billing fetches the authenticated owner's order.
	http.Redirect(w, r, location, http.StatusSeeOther)
}
