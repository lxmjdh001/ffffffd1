package platform

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Wire-compatible with go7/internal/service/futoon_pay.go.
// MD5 is required by this legacy gateway protocol, not used for passwords.
func FutoonSign(params map[string]string, secret string) string {
	keys := []string{}
	for k, v := range params {
		if k != "sign" && k != "sign_type" && v != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := []string{}
	for _, k := range keys {
		parts = append(parts, k+"="+strings.TrimSpace(params[k]))
	}
	sum := md5.Sum([]byte(strings.Join(parts, "&") + secret))
	return hex.EncodeToString(sum[:])
}
func MonitorSign(params map[string]string, timestamp, secret string) string {
	keys := []string{}
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{}
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	raw := strings.Join(parts, "&") + "&timestamp=" + timestamp + "&secret=" + secret
	h := hmac.New(sha256.New, []byte(secret))
	_, _ = h.Write([]byte(raw))
	return hex.EncodeToString(h.Sum(nil))
}
func (s *Server) paymentURL(o Order, ip string) string {
	kind := o.Method
	if kind == "wechat" {
		kind = "wxpay"
	}
	payAmount := formatCents(o.AmountMicros)
	returnURL := s.Config.PublicURL + "/#/billing?order=" + o.ID
	if o.PaymentCurrency == "CNY" {
		payAmount = formatCNYCents(o.PayCNYCents)
		returnURL = s.Config.PublicURL + "/api/payments/futoon/return"
	}
	params := map[string]string{"pid": s.Config.FutoonPID, "type": kind, "out_trade_no": o.ID, "notify_url": s.Config.PublicURL + "/api/payments/futoon/notify", "return_url": returnURL, "name": "D1 Apk 余额充值", "money": payAmount, "clientip": ip, "device": "pc"}
	params["sign"] = FutoonSign(params, s.Config.FutoonKey)
	params["sign_type"] = "MD5"
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	return s.Config.FutoonURL + "?" + q.Encode()
}

func rechargeBonus(amount int64, settings Settings) int64 {
	if !settings.RechargeBonusEnabled {
		return 0
	}
	var threshold, percent int64
	for _, tier := range settings.RechargeBonusTiers {
		if tier.ThresholdMicros >= Dollar && amount >= tier.ThresholdMicros && tier.ThresholdMicros > threshold && tier.Percent >= 1 && tier.Percent <= 100 {
			threshold, percent = tier.ThresholdMicros, tier.Percent
		}
	}
	return amount * percent / 100
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	key, e := idempotency(r)
	if e != nil {
		fail(w, e)
		return
	}
	var in struct {
		Amount string `json:"amount"`
		Method string `json:"method"`
	}
	if e = decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	amount, e := ParseMoney(in.Amount)
	if e != nil || amount < Dollar || amount > 10000*Dollar || amount%10000 != 0 {
		fail(w, problem(400, "单笔充值须为 50–10,000 美元，最多两位小数"))
		return
	}
	if in.Method != "manual" && in.Method != "usdt" && in.Method != "alipay" {
		fail(w, problem(400, "不支持的支付方式"))
		return
	}
	// Retrying a successful request must return its original order even if the
	// operator has since changed the wallet or the monitor is temporarily down.
	var existing Order
	err := s.DB.First(&existing, "idempotency_key = ?", userOf(r).ID+":"+key).Error
	if err == nil {
		if existing.Method != in.Method || existing.AmountMicros != amount {
			fail(w, problem(409, "幂等键已用于另一笔请求"))
			return
		}
		jsonOut(w, 201, existing)
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		fail(w, err)
		return
	}
	if amount < 50*Dollar {
		fail(w, problem(400, "单笔充值须为 50–10,000 美元，最多两位小数"))
		return
	}
	// Keep idempotent access to historical Alipay orders above, but never
	// create another gateway order after the channel has been retired.
	if in.Method == "alipay" {
		fail(w, problem(400, "支付宝充值已停用，请选择 USDT 或人工充值"))
		return
	}
	if e = s.rate("order:"+userOf(r).ID, 20, time.Hour); e != nil {
		fail(w, e)
		return
	}
	if in.Method == "usdt" {
		settings, err := getSettings(s.DB)
		if err != nil {
			fail(w, err)
			return
		}
		wallet := s.Config.receivingWallet(settings)
		if wallet == "" {
			fail(w, problem(503, "该支付渠道尚未配置"))
			return
		}
		if s.Config.usdtProvider() == "uzf" {
			// Check the existing signed query service before displaying a new
			// payment order. Do not require changes to UZF or its OKX account.
			if _, err := s.queryUZF(r.Context(), Order{Wallet: wallet, USDTUnits: 1, CreatedAt: s.Now(), ExpiresAt: s.Now()}); err != nil {
				fail(w, err)
				return
			}
		}
	}
	var order Order
	e = s.DB.Transaction(func(tx *gorm.DB) error {
		u, e := lockUser(tx, userOf(r).ID)
		if e != nil {
			return e
		}
		idkey := u.ID + ":" + key
		e = tx.First(&order, "idempotency_key = ?", idkey).Error
		if e == nil {
			if order.Method != in.Method || order.AmountMicros != amount {
				return problem(409, "幂等键已用于另一笔请求")
			}
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		settings, e := getSettings(tx)
		if e != nil {
			return e
		}
		order = Order{ID: randomHex(16), UserID: u.ID, IdempotencyKey: idkey, Method: in.Method, AmountMicros: amount, Status: "pending", CreatedAt: s.Now(), ExpiresAt: s.Now().Add(30 * time.Minute)}
		order.BonusMicros = rechargeBonus(amount, settings)
		if in.Method == "manual" {
			order.ExpiresAt = s.Now().Add(7 * 24 * time.Hour)
		}
		if in.Method == "usdt" {
			order.Wallet = s.Config.receivingWallet(settings)
			if order.Wallet == "" {
				return problem(503, "该支付渠道尚未配置")
			}
			order.USDTProvider = s.Config.usdtProvider()
			order.RateMicros = settings.USDTRateMicros
			if e = s.reserveUSDT(tx, &order); e != nil {
				return e
			}
		}
		return tx.Create(&order).Error
	})
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 201, order)
}
func (s *Server) reserveUSDT(tx *gorm.DB, o *Order) error {
	if o.RateMicros <= 0 {
		return problem(400, "USDT 充值汇率无效")
	}
	// Allocate whole cents from base - 0.99U through base + 0.10U.
	// For 500 USD at a 1:1 rate this is exactly 499.01–500.10 USDT.
	// Keep immutable reservations, including historical six-decimal amounts,
	// so a delayed payment can never be reassigned to a different D1 order.
	baseCents := (o.AmountMicros*100 + o.RateMicros - 1) / o.RateMicros
	low, high := max(int64(1), baseCents-99), baseCents+10
	slots := high - low + 1
	start, e := rand.Int(rand.Reader, big.NewInt(slots))
	if e != nil {
		return e
	}
	// Randomize the starting point, then visit every slot exactly once.
	// A collision must not hide another available amount in the same range.
	for i := int64(0); i < slots; i++ {
		units := (low + (start.Int64()+i)%slots) * 10000
		reservation := USDTReservation{Wallet: o.Wallet, Units: units, OrderID: o.ID}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&reservation)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 1 {
			o.USDTUnits = units
			return nil
		}
	}
	return problem(503, "当前充值金额的支付席位已满，请更换金额")
}
func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	listOut[Order](w, r, s.DB.Model(&Order{}).Where("user_id = ?", userOf(r).ID))
}
func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	var o Order
	if e := s.DB.First(&o, "id = ? AND user_id = ?", r.PathValue("id"), userOf(r).ID).Error; e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, o)
}
func (s *Server) settle(orderID, tradeID, method string, amount int64) error {
	return s.settleAudited(orderID, tradeID, method, amount, "", "")
}
func (s *Server) settleAudited(orderID, tradeID, method string, amount int64, actor, note string) error {
	return s.settleChecked(orderID, tradeID, method, amount, actor, note, false)
}

func (s *Server) settleChecked(orderID, tradeID, method string, amount int64, actor, note string, gatewayAmount bool) error {
	var pre Order
	if e := s.DB.First(&pre, "id = ?", orderID).Error; e != nil {
		return e
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		ancestors, e := referralAncestors(tx, pre.UserID)
		if e != nil {
			return e
		}
		users, e := lockRechargeUsers(tx, pre.UserID, ancestors)
		if e != nil {
			return e
		}
		u := users[pre.UserID]
		var o Order
		if e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&o, "id = ?", orderID).Error; e != nil {
			return e
		}
		expected := o.AmountMicros
		if gatewayAmount && o.PaymentCurrency == "CNY" {
			fee, cents, err := alipayQuote(o.AmountMicros, o.AlipayRateMicros)
			if err != nil || o.Method != "alipay" || fee != o.FeeMicros || cents != o.PayCNYCents {
				return problem(400, "订单付款快照无效")
			}
			expected = o.PayCNYCents * 10000
		}
		if o.Method != method || expected != amount || tradeID == "" || o.BonusMicros < 0 || o.BonusMicros > o.AmountMicros {
			return problem(400, "支付信息不匹配")
		}
		if o.Status == "paid" {
			// An administrator may retry a confirmation with optional fields
			// changed, or after the payment callback has already credited it.
			if actor != "" || (o.TradeID != nil && *o.TradeID == tradeID) {
				return nil
			}
			return problem(409, "订单已通过其他交易结算")
		}
		if o.Status != "pending" && o.Status != "expired" {
			return problem(409, "订单当前不可结算")
		}
		now := s.Now()
		o.Status = "paid"
		o.PaidAt = &now
		o.TradeID = &tradeID
		if e = tx.Save(&o).Error; e != nil {
			if errors.Is(e, gorm.ErrDuplicatedKey) {
				return problem(409, "该交易号已用于其他订单")
			}
			return e
		}
		if e = changeBalance(tx, u, o.AmountMicros, "recharge:"+o.ID, "recharge", "余额充值 · "+o.Method); e != nil {
			return e
		}
		if o.BonusMicros > 0 {
			if e = changeBalance(tx, u, o.BonusMicros, "recharge-bonus:"+o.ID, "recharge_bonus", "充值活动赠送"); e != nil {
				return e
			}
		}
		if e = creditReferralRewards(tx, o, ancestors, users, now); e != nil {
			return e
		}
		if actor != "" {
			return recordAudit(tx, actor, "order.settle", o.ID, tradeID+" · "+note)
		}
		return nil
	})
}
func (s *Server) futoonNotify(w http.ResponseWriter, r *http.Request) {
	if s.Config.FutoonKey == "" {
		http.Error(w, "fail", 503)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	if e := r.ParseForm(); e != nil {
		http.Error(w, "fail", 400)
		return
	}
	params := map[string]string{}
	for k, vs := range r.Form {
		if len(vs) != 1 {
			http.Error(w, "fail", 400)
			return
		}
		params[k] = vs[0]
	}
	if params["pid"] != s.Config.FutoonPID || params["sign_type"] != "MD5" || subtle.ConstantTimeCompare([]byte(params["sign"]), []byte(FutoonSign(params, s.Config.FutoonKey))) != 1 || params["trade_status"] != "TRADE_SUCCESS" {
		http.Error(w, "fail", 400)
		return
	}
	amount, e := ParseMoney(params["money"])
	trade := params["trade_no"]
	if e != nil || amount%10000 != 0 || len(trade) < 1 || len(trade) > 100 || strings.TrimSpace(trade) != trade {
		http.Error(w, "fail", 400)
		return
	}
	method := params["type"]
	if method == "wxpay" {
		method = "wechat"
	}
	if method != "alipay" && method != "wechat" {
		http.Error(w, "fail", 400)
		return
	}
	if e = s.settleChecked(params["out_trade_no"], "futoon:"+trade, method, amount, "", "", true); e != nil {
		http.Error(w, "fail", 400)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "success")
}

const usdtContract = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"

func validTronAddress(address string) bool {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	if len(address) != 34 || address[0] != 'T' {
		return false
	}
	n := new(big.Int)
	for _, r := range address {
		i := strings.IndexRune(alphabet, r)
		if i < 0 {
			return false
		}
		n.Mul(n, big.NewInt(58))
		n.Add(n, big.NewInt(int64(i)))
	}
	b := n.Bytes()
	if len(b) != 25 || b[0] != 0x41 {
		return false
	}
	a := sha256.Sum256(b[:21])
	c := sha256.Sum256(a[:])
	return subtle.ConstantTimeCompare(c[:4], b[21:]) == 1
}

type tronTransfer struct {
	TransactionID  string `json:"transaction_id"`
	To             string `json:"to"`
	Value          string `json:"value"`
	Type           string `json:"type"`
	BlockTimestamp int64  `json:"block_timestamp"`
	TokenInfo      struct {
		Address  string `json:"address"`
		Decimals int    `json:"decimals"`
	} `json:"token_info"`
}

func validateTransfer(o Order, t tronTransfer) bool {
	units, e := strconv.ParseInt(t.Value, 10, 64)
	if e != nil {
		return false
	}
	return len(t.TransactionID) == 64 && hexKey.MatchString(t.TransactionID) && t.Type == "Transfer" && t.To == o.Wallet && units == o.USDTUnits && t.TokenInfo.Address == usdtContract && t.TokenInfo.Decimals == 6 && t.BlockTimestamp >= o.CreatedAt.UnixMilli() && t.BlockTimestamp <= o.ExpiresAt.UnixMilli()
}

// The go7 monitor's data.found is only a discovery hint. It is never sufficient
// evidence to credit money; a confirmed, matching mainnet transfer is required.
func (s *Server) monitorHint(ctx context.Context, o Order) {
	if s.Config.USDTMonitorURL == "" {
		return
	}
	params := map[string]string{"amount": formatMoney(o.USDTUnits), "currency": "USDT"}
	ts := strconv.FormatInt(s.Now().Unix(), 10)
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	q.Set("timestamp", ts)
	q.Set("signature", MonitorSign(params, ts, s.Config.USDTMonitorSecret))
	req, e := http.NewRequestWithContext(ctx, "GET", s.Config.USDTMonitorURL+"?"+q.Encode(), nil)
	if e != nil {
		return
	}
	resp, e := s.HTTP.Do(req)
	if e == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 65536))
		_ = resp.Body.Close()
	}
}
func (s *Server) reconcileUSDT(ctx context.Context, o Order) error {
	if o.Method != "usdt" || o.Status == "paid" {
		return nil
	}
	if o.CreatedAt.Before(s.Now().Add(-48 * time.Hour)) {
		return problem(400, "订单已超过自动核对窗口，请联系管理员")
	}
	if o.USDTProvider == "uzf" {
		return s.reconcileUZF(ctx, o)
	}
	if o.USDTProvider != "" && o.USDTProvider != "tron" {
		return problem(503, "订单支付渠道配置无效，请联系管理员")
	}
	s.monitorHint(ctx, o)
	q := url.Values{"only_confirmed": {"true"}, "only_to": {"true"}, "contract_address": {usdtContract}, "min_timestamp": {strconv.FormatInt(o.CreatedAt.UnixMilli(), 10)}, "max_timestamp": {strconv.FormatInt(o.ExpiresAt.UnixMilli(), 10)}, "limit": {"200"}, "order_by": {"block_timestamp,asc"}}
	for page := 0; page < 20; page++ {
		req, e := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(s.Config.TronAPIURL, "/")+"/v1/accounts/"+url.PathEscape(o.Wallet)+"/transactions/trc20?"+q.Encode(), nil)
		if e != nil {
			return e
		}
		if s.Config.TronAPIKey != "" {
			req.Header.Set("TRON-PRO-API-KEY", s.Config.TronAPIKey)
		}
		resp, e := s.HTTP.Do(req)
		if e != nil {
			return problem(503, "链上查询暂时不可用，请稍后再试")
		}
		var result struct {
			Success bool           `json:"success"`
			Data    []tronTransfer `json:"data"`
			Meta    struct {
				Fingerprint string `json:"fingerprint"`
			} `json:"meta"`
		}
		err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&result)
		_ = resp.Body.Close()
		if resp.StatusCode != 200 || err != nil || !result.Success {
			return problem(503, "链上查询暂时不可用，请稍后再试")
		}
		for _, t := range result.Data {
			if validateTransfer(o, t) {
				return s.settle(o.ID, "tron:"+t.TransactionID, "usdt", o.AmountMicros)
			}
		}
		if result.Meta.Fingerprint == "" || len(result.Data) == 0 {
			return nil
		}
		q.Set("fingerprint", result.Meta.Fingerprint)
	}
	return problem(503, "交易记录较多，待后台继续核对")
}
func (s *Server) checkOrder(w http.ResponseWriter, r *http.Request) {
	if e := s.rate("order-check:"+userOf(r).ID, 6, time.Minute); e != nil {
		fail(w, e)
		return
	}
	var o Order
	if e := s.DB.First(&o, "id = ? AND user_id = ?", r.PathValue("id"), userOf(r).ID).Error; e != nil {
		fail(w, e)
		return
	}
	if e := s.reconcileUSDT(r.Context(), o); e != nil {
		fail(w, e)
		return
	}
	if e := s.DB.First(&o, "id = ?", o.ID).Error; e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, o)
}
