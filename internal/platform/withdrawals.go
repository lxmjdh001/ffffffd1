package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const referralWithdrawalMinimum int64 = 100 * Dollar

// This provenance counter is part of the existing balance, not a second credit.
// Spending consumes commission first, so recharges and gifts cannot turn spent
// commission back into cashable funds. Every writer already holds the User row.
type ReferralWallet struct {
	UserID                 string `gorm:"primaryKey;size:32"`
	AvailableMicros        int64  `gorm:"not null;default:0;check:available_micros >= 0"`
	LedgerCount            int64  `gorm:"not null;default:0;check:ledger_count >= 0"`
	LedgerFingerprint      string `gorm:"size:64;not null"`
	ReconciliationRequired bool   `gorm:"not null;default:false"`
}

type ReferralWithdrawal struct {
	ID              string     `json:"id" gorm:"primaryKey;size:32"`
	UserID          string     `json:"user_id" gorm:"index:idx_withdrawal_owner_time,priority:1;uniqueIndex:idx_withdrawal_owner_key,priority:1;size:32;not null"`
	IdempotencyKey  string     `json:"-" gorm:"uniqueIndex:idx_withdrawal_owner_key,priority:2;size:64;not null"`
	AmountMicros    int64      `json:"amount_micros" gorm:"not null;check:amount_micros >= 100000000"`
	Address         string     `json:"address" gorm:"size:34;not null"`
	Status          string     `json:"status" gorm:"index;size:16;not null;check:status IN ('pending','approved','paid','rejected')"`
	ReviewNote      string     `json:"review_note" gorm:"size:1000"`
	PayoutReference *string    `json:"payout_reference" gorm:"uniqueIndex;size:64"`
	CreatedAt       time.Time  `json:"created_at" gorm:"index:idx_withdrawal_owner_time,priority:2"`
	ReviewedAt      *time.Time `json:"reviewed_at"`
	PaidAt          *time.Time `json:"paid_at"`
	ReviewedBy      string     `json:"-" gorm:"size:32"`
	PaidBy          string     `json:"-" gorm:"size:32"`
}

func migrateReferralWithdrawals(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = 11").Count(&count).Error; err != nil || count > 0 {
		return err
	}
	if err := tx.AutoMigrate(&ReferralWallet{}, &ReferralWithdrawal{}); err != nil {
		return err
	}
	// Replay only provenance; never alter existing money, reservations, orders,
	// or ledger rows. Equal-time credits precede debits to avoid overestimating
	// remaining commission when the old rows have no ordering sequence.
	lastID := ""
	for {
		var users []User
		if err := tx.Where("id > ? AND EXISTS (SELECT 1 FROM ledgers WHERE ledgers.user_id = users.id AND ledgers.kind = ? AND ledgers.amount_micros > 0)", lastID, "referral_reward").Order("id").Limit(200).Find(&users).Error; err != nil {
			return err
		}
		if len(users) == 0 {
			break
		}
		for _, user := range users {
			available, err := replayReferralWallet(tx, user)
			if err != nil {
				return err
			}
			count, digest, err := referralLedgerProof(tx, user.ID)
			if err != nil {
				return err
			}
			wallet := ReferralWallet{UserID: user.ID, AvailableMicros: available, LedgerCount: count, LedgerFingerprint: digest}
			if err := tx.Create(&wallet).Error; err != nil {
				return err
			}
		}
		lastID = users[len(users)-1].ID
	}
	return tx.Create(&Migration{Version: 11, AppliedAt: time.Now().UTC()}).Error
}

func referralWallet(tx *gorm.DB, userID string) (ReferralWallet, error) {
	wallet := ReferralWallet{UserID: userID}
	err := tx.First(&wallet, "user_id = ?", userID).Error
	if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
		return wallet, err
	}
	// Capture history before the first new ledger is written. Historical cash
	// eligibility is seeded by migration only; lazy initialization grants zero.
	count, digest, err := referralLedgerProof(tx, userID)
	if err != nil {
		return wallet, err
	}
	wallet.LedgerCount, wallet.LedgerFingerprint = count, digest
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&wallet).Error; err != nil {
		return wallet, err
	}
	err = tx.First(&wallet, "user_id = ?", userID).Error
	return wallet, err
}

func referralLedgerDigest(entry Ledger) [32]byte {
	// Fixed JSON fields and an integer amount make this stable across database
	// timestamp precision. XOR advances the proof without scanning downloads.
	raw, _ := json.Marshal(struct {
		ID, UserID, Reference, Kind string
		AmountMicros                int64
	}{entry.ID, entry.UserID, entry.Reference, entry.Kind, entry.AmountMicros})
	return sha256.Sum256(raw)
}

func referralLedgerProof(tx *gorm.DB, userID string) (int64, string, error) {
	rows, err := tx.Model(&Ledger{}).Select("id, user_id, reference, kind, amount_micros").Where("user_id = ?", userID).Rows()
	if err != nil {
		return 0, "", err
	}
	defer rows.Close()
	var count int64
	var proof [32]byte
	for rows.Next() {
		var entry Ledger
		if err := tx.ScanRows(rows, &entry); err != nil {
			return 0, "", err
		}
		digest := referralLedgerDigest(entry)
		for i := range proof {
			proof[i] ^= digest[i]
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, "", err
	}
	return count, hex.EncodeToString(proof[:]), nil
}

func replayReferralWallet(tx *gorm.DB, user User) (int64, error) {
	var ledger []Ledger
	if err := tx.Select("kind, amount_micros").Where("user_id = ? AND (amount_micros < 0 OR kind IN ?)", user.ID, []string{"referral_reward", "withdrawal_refund"}).Order("created_at ASC").Order("CASE WHEN amount_micros > 0 THEN 0 ELSE 1 END ASC").Order("id ASC").Find(&ledger).Error; err != nil {
		return 0, err
	}
	var available int64
	for _, row := range ledger {
		if row.AmountMicros > 0 && (row.Kind == "referral_reward" || row.Kind == "withdrawal_refund") {
			available += min(row.AmountMicros, 100000000*Dollar-available)
		} else if row.AmountMicros < 0 {
			if row.AmountMicros < -available {
				available = 0
			} else {
				available += row.AmountMicros
			}
		}
	}
	return min(available, user.BalanceMicros), nil
}

func updateReferralWallet(tx *gorm.DB, user *User, wallet ReferralWallet, entry Ledger) error {
	delta, kind := entry.AmountMicros, entry.Kind
	available := wallet.AvailableMicros
	if delta < 0 {
		if delta < -available {
			available = 0
		} else {
			available += delta
		}
	} else if kind == "referral_reward" || kind == "withdrawal_refund" {
		available += min(delta, 100000000*Dollar-available)
	}
	available = min(available, user.BalanceMicros)
	proof, err := hex.DecodeString(wallet.LedgerFingerprint)
	if err != nil || len(proof) != 32 {
		// Damaged metadata disables withdrawals while ordinary balance activity
		// and rejection refunds continue to work.
		proof = make([]byte, 32)
		wallet.ReconciliationRequired = true
	}
	digest := referralLedgerDigest(entry)
	for i := range proof {
		proof[i] ^= digest[i]
	}
	return tx.Model(&wallet).Updates(map[string]any{
		"available_micros": available, "ledger_count": wallet.LedgerCount + 1,
		"ledger_fingerprint": hex.EncodeToString(proof), "reconciliation_required": wallet.ReconciliationRequired,
	}).Error
}

func referralWithdrawalAvailable(tx *gorm.DB, user User) (int64, bool, error) {
	wallet, err := referralWallet(tx, user.ID)
	if err != nil {
		return 0, false, err
	}
	if wallet.ReconciliationRequired {
		return 0, true, nil
	}
	// Compare the exact append-only ledger baseline. Replaying history against
	// a later balance could revive funds clipped at migration. Old binaries or
	// operator changes to ledger history require a separate manual audit.
	count, fingerprint, err := referralLedgerProof(tx, user.ID)
	if err != nil {
		return 0, false, err
	}
	if count != wallet.LedgerCount || fingerprint != wallet.LedgerFingerprint {
		if err := tx.Model(&wallet).Update("reconciliation_required", true).Error; err != nil {
			return 0, false, err
		}
		return 0, true, nil
	}
	return min(wallet.AvailableMicros, max(int64(0), user.BalanceMicros-user.HeldMicros)), false, nil
}

func (s *Server) listReferralWithdrawals(w http.ResponseWriter, r *http.Request) {
	q := s.DB.Model(&ReferralWithdrawal{}).Where("user_id = ?", userOf(r).ID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		fail(w, err)
		return
	}
	offset, limit := referralPageParams(r, "page")
	items := []ReferralWithdrawal{}
	if err := q.Order("created_at DESC, id DESC").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, referralPage[ReferralWithdrawal]{Data: items, Total: total})
}

func (s *Server) requestReferralWithdrawal(w http.ResponseWriter, r *http.Request) {
	key, err := idempotency(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Amount  string `json:"amount"`
		Address string `json:"address"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	amount, err := ParseMoney(in.Amount)
	in.Address = strings.TrimSpace(in.Address)
	if err != nil || amount < referralWithdrawalMinimum {
		fail(w, problem(400, "提现金额至少为 100 USD，最多支持 6 位小数"))
		return
	}
	if !validTronAddress(in.Address) {
		fail(w, problem(400, "请输入有效的 USDT（TRC20）收款地址"))
		return
	}
	var out ReferralWithdrawal
	created := false
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		user, err := lockUser(tx, userOf(r).ID)
		if err != nil {
			return err
		}
		if user.Disabled {
			return problem(403, "账户不可用")
		}
		err = tx.First(&out, "user_id = ? AND idempotency_key = ?", user.ID, key).Error
		if err == nil {
			if out.AmountMicros != amount || out.Address != in.Address {
				return problem(409, "幂等键对应的提现内容不一致")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		available, required, err := referralWithdrawalAvailable(tx, user)
		if err != nil {
			return err
		}
		if required {
			return problem(409, "提现资格需要人工核对，请联系客服")
		}
		if amount > available {
			return problem(400, "可提现返利额度不足，已消费或冻结的额度不可提现")
		}
		out = ReferralWithdrawal{ID: randomHex(16), UserID: user.ID, IdempotencyKey: key, AmountMicros: amount, Address: in.Address, Status: "pending", CreatedAt: s.Now()}
		if err := tx.Create(&out).Error; err != nil {
			return err
		}
		if err := changeBalance(tx, &user, -amount, "withdrawal-hold:"+out.ID, "withdrawal_hold", "邀请返利提现申请 · 等待人工审核"); err != nil {
			return err
		}
		created = true
		return recordAudit(tx, user.ID, "referral.withdrawal.request", out.ID, formatMoney(amount)+" USD · TRC20 · pending")
	})
	if err != nil {
		fail(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	jsonOut(w, status, out)
}

type adminReferralWithdrawal struct {
	ReferralWithdrawal
	UserName  string `json:"user_name"`
	UserEmail string `json:"user_email"`
}

func (s *Server) adminReferralWithdrawals(w http.ResponseWriter, r *http.Request) {
	q := s.DB.Model(&ReferralWithdrawal{}).Joins("JOIN users ON users.id = referral_withdrawals.user_id")
	if status := r.URL.Query().Get("status"); status != "" {
		if status != "pending" && status != "approved" && status != "paid" && status != "rejected" {
			fail(w, problem(400, "提现状态无效"))
			return
		}
		q = q.Where("referral_withdrawals.status = ?", status)
	}
	q = addQuery(q, r, "users.name", "users.email", "referral_withdrawals.id", "referral_withdrawals.address", "referral_withdrawals.payout_reference")
	var total int64
	if err := q.Count(&total).Error; err != nil {
		fail(w, err)
		return
	}
	offset, limit := pageParams(r)
	items := []adminReferralWithdrawal{}
	if err := q.Select("referral_withdrawals.*, users.name AS user_name, users.email AS user_email").Order("referral_withdrawals.created_at DESC, referral_withdrawals.id DESC").Offset(offset).Limit(limit).Scan(&items).Error; err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, referralPage[adminReferralWithdrawal]{Data: items, Total: total})
}

func (s *Server) reviewReferralWithdrawal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action        string `json:"action"`
		Note          string `json:"note"`
		TransactionID string `json:"transaction_id"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	in.TransactionID = strings.ToLower(strings.TrimSpace(in.TransactionID))
	if utf8.RuneCountInString(in.Note) > 1000 || strings.ContainsRune(in.Note, '\x00') {
		fail(w, problem(400, "审核备注过长或包含无效字符"))
		return
	}
	if in.Action != "approve" && in.Action != "reject" && in.Action != "paid" {
		fail(w, problem(400, "审核操作无效"))
		return
	}
	if in.Action == "reject" && in.Note == "" {
		fail(w, problem(400, "请填写拒绝提现的原因"))
		return
	}
	if in.Action == "paid" && (len(in.TransactionID) != 64 || !hexKey.MatchString(in.TransactionID)) {
		fail(w, problem(400, "请填写 64 位 TRC20 链上交易哈希"))
		return
	}
	if in.Action != "paid" && in.TransactionID != "" {
		fail(w, problem(400, "此审核操作不能填写付款凭证"))
		return
	}
	// Read the immutable owner, then follow the global lock order: User first,
	// withdrawal second. This never holds a request row while waiting for money.
	var original ReferralWithdrawal
	if err := s.DB.First(&original, "id = ?", r.PathValue("id")).Error; err != nil {
		fail(w, err)
		return
	}
	var out ReferralWithdrawal
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		user, err := lockUser(tx, original.UserID)
		if err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&out, "id = ? AND user_id = ?", original.ID, user.ID).Error; err != nil {
			return err
		}
		want := map[string]string{"approve": "approved", "reject": "rejected", "paid": "paid"}[in.Action]
		if out.Status == want {
			if out.ReviewNote != in.Note || (in.Action == "paid" && (out.PayoutReference == nil || *out.PayoutReference != in.TransactionID)) {
				return problem(409, "该提现已处理，请刷新后查看记录")
			}
			return nil
		}
		now := s.Now()
		switch in.Action {
		case "approve":
			if out.Status != "pending" {
				return problem(409, "只能审核待审核的提现申请")
			}
			out.ReviewedBy, out.ReviewedAt = userOf(r).ID, &now
		case "reject":
			if out.Status != "pending" && out.Status != "approved" {
				return problem(409, "该提现已结束，不能重复退回额度")
			}
			if err := changeBalance(tx, &user, out.AmountMicros, "withdrawal-refund:"+out.ID, "withdrawal_refund", "邀请返利提现退回 · "+in.Note); err != nil {
				return err
			}
			out.ReviewedBy, out.ReviewedAt = userOf(r).ID, &now
		case "paid":
			if out.Status != "approved" {
				return problem(409, "请先审核通过，再确认线下打款")
			}
			out.PaidBy, out.PaidAt, out.PayoutReference = userOf(r).ID, &now, &in.TransactionID
		}
		out.Status, out.ReviewNote = want, in.Note
		if err := tx.Save(&out).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return problem(409, "该链上交易哈希已用于其他提现记录")
			}
			return err
		}
		return recordAudit(tx, userOf(r).ID, "referral.withdrawal."+in.Action, out.ID, formatMoney(out.AmountMicros)+" USD · "+want+" · "+in.Note)
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, out)
}
