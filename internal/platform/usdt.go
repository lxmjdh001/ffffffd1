package platform

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func migrateUSDTPayment(tx *gorm.DB) error {
	var n int64
	if err := tx.Model(&Migration{}).Where("version = 8").Count(&n).Error; err != nil || n > 0 {
		return err
	}
	if err := tx.AutoMigrate(&Order{}, &Settings{}); err != nil {
		return err
	}
	return tx.Create(&Migration{Version: 8, AppliedAt: time.Now().UTC()}).Error
}

type uzfReceipt struct {
	BillID        string      `json:"bill_id"`
	Amount        json.Number `json:"amount"`
	Currency      string      `json:"currency"`
	BillTimestamp int64       `json:"bill_timestamp"`
}

type uzfResult struct {
	Success bool `json:"success"`
	Data    struct {
		Found    bool        `json:"found"`
		Transfer *uzfReceipt `json:"transfer"`
	} `json:"data"`
}

func (s *Server) queryUZF(ctx context.Context, o Order) (uzfResult, error) {
	var result uzfResult
	if s.Config.USDTMonitorURL == "" || s.Config.USDTMonitorSecret == "" {
		return result, problem(503, "USDT 到账监控尚未配置")
	}
	// Match go7 and the existing UZF API exactly; no new server parameters,
	// endpoints, OKX credentials or deposit-record schema are required.
	params := map[string]string{"amount": formatMoney(o.USDTUnits), "currency": "USDT"}
	ts := strconv.FormatInt(s.Now().Unix(), 10)
	q := url.Values{}
	for key, value := range params {
		q.Set(key, value)
	}
	q.Set("timestamp", ts)
	q.Set("signature", MonitorSign(params, ts, s.Config.USDTMonitorSecret))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Config.USDTMonitorURL+"?"+q.Encode(), nil)
	if err != nil {
		return result, problem(503, "USDT 到账监控配置无效")
	}
	client := *s.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return result, problem(503, "USDT 到账监控暂时不可用，请稍后重试")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result) != nil || !result.Success {
		return result, problem(503, "USDT 查询服务暂时不可用，请检查现有 UZF 查询地址和密钥")
	}
	return result, nil
}

func validUZFReceipt(o Order, receipt uzfReceipt) bool {
	units, err := ParseMoney(receipt.Amount.String())
	return err == nil && units == o.USDTUnits && receipt.Currency == "USDT" &&
		receipt.BillID != "" && len(receipt.BillID) <= 100 &&
		receipt.BillTimestamp >= o.CreatedAt.UnixMilli() && receipt.BillTimestamp <= o.ExpiresAt.UnixMilli()
}

func (s *Server) reconcileUZF(ctx context.Context, o Order) error {
	result, err := s.queryUZF(ctx, o)
	if err != nil || !result.Data.Found {
		return err
	}
	if result.Data.Transfer == nil || !validUZFReceipt(o, *result.Data.Transfer) {
		return problem(502, "USDT 付款凭证不匹配，未自动入账，请联系管理员")
	}
	// Original UZF returns an OKX bill ID, not a blockchain transaction ID.
	// The existing globally unique trade ID prevents the same bill being used
	// by another user/order, even after the administrator changes the wallet.
	return s.settle(o.ID, "uzf:"+result.Data.Transfer.BillID, "usdt", o.AmountMicros)
}

func (s *Server) updateUSDTWallet(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Wallet   string `json:"wallet"`
		Password string `json:"secondary_password"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if err := s.rate("usdt-wallet-password:"+userOf(r).ID, 5, 15*time.Minute); err != nil {
		fail(w, err)
		return
	}
	if s.Config.USDTAddressPasswordHash == "" {
		fail(w, problem(503, "收款地址二级验证尚未配置，请联系运维"))
		return
	}
	if len(in.Password) > 72 || bcrypt.CompareHashAndPassword([]byte(s.Config.USDTAddressPasswordHash), []byte(in.Password)) != nil {
		fail(w, problem(403, "二级密码错误，收款地址未修改"))
		return
	}
	if in.Wallet != "" && !validTronAddress(in.Wallet) {
		fail(w, problem(400, "请输入有效的 TRC20 主网收款地址，请勿包含空格"))
		return
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("UPDATE settings SET id = id WHERE id = 1").Error; err != nil {
			return err
		}
		previous, err := getSettings(tx)
		if err != nil {
			return err
		}
		if err := tx.Model(&Settings{}).Where("id = 1").Update("usdt_wallet", in.Wallet).Error; err != nil {
			return err
		}
		// Only public receiving addresses are audited. Never log the password.
		return recordAudit(tx, userOf(r).ID, "settings.usdt_wallet.change", "platform", s.Config.receivingWallet(previous)+" -> "+in.Wallet)
	})
	if err != nil {
		fail(w, err)
		return
	}
	s.adminSettings(w, r)
}
