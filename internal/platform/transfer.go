package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

type byteRange struct {
	Start int64 `json:"s"`
	End   int64 `json:"e"`
}

func mergeRanges(ranges []byteRange, next byteRange) []byteRange {
	ranges = append(ranges, next)
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })
	out := make([]byteRange, 0, len(ranges))
	for _, r := range ranges {
		if len(out) == 0 || out[len(out)-1].End+1 < r.Start {
			out = append(out, r)
		} else if r.End > out[len(out)-1].End {
			out[len(out)-1].End = r.End
		}
	}
	return out
}
func cleanText(v string, max int) string {
	v = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, v)
	if len(v) > max {
		v = v[:max]
		for !validUTF8(v) {
			v = v[:len(v)-1]
		}
	}
	return v
}
func safeSource(raw string) string {
	u, e := url.Parse(raw)
	if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return ""
	}
	u.User = nil
	u.Fragment = ""
	return cleanText(u.String(), 2048)
}
func classifyDevice(ua string) (device, os, browser string) {
	u := strings.ToLower(ua)
	device = "桌面"
	os = "未知"
	browser = "未知"
	switch {
	case strings.Contains(u, "bot") || strings.Contains(u, "spider") || strings.Contains(u, "crawler"):
		device = "机器人"
	case strings.Contains(u, "ipad") || (strings.Contains(u, "android") && !strings.Contains(u, "mobile")):
		device = "平板"
	case strings.Contains(u, "iphone") || strings.Contains(u, "mobile"):
		device = "手机"
	}
	switch {
	case strings.Contains(u, "android"):
		os = "Android"
	case strings.Contains(u, "iphone") || strings.Contains(u, "ipad"):
		os = "iOS"
	case strings.Contains(u, "windows"):
		os = "Windows"
	case strings.Contains(u, "macintosh") || strings.Contains(u, "mac os"):
		os = "macOS"
	case strings.Contains(u, "linux"):
		os = "Linux"
	}
	switch {
	case strings.Contains(u, "micromessenger"):
		browser = "微信"
	case strings.Contains(u, "fban") || strings.Contains(u, "fbav"):
		browser = "Facebook"
	case strings.Contains(u, "tiktok") || strings.Contains(u, "musical_ly"):
		browser = "TikTok"
	case strings.Contains(u, "edg/"):
		browser = "Edge"
	case strings.Contains(u, "firefox/"):
		browser = "Firefox"
	case strings.Contains(u, "chrome/") || strings.Contains(u, "crios/"):
		browser = "Chrome"
	case strings.Contains(u, "safari/"):
		browser = "Safari"
	}
	return
}

// Reservation and settlement use the same owner-row lock. It is held only
// during short transactions, never while an application file is streamed.
func (s *Server) reserveDownload(t Ticket) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		u, e := lockUser(tx, t.UserID)
		if e != nil {
			return e
		}
		if u.Disabled {
			return problem(403, "应用发布者账户已停用")
		}
		var ticket Ticket
		if e = tx.First(&ticket, "hash = ? AND expires_at > ?", t.Hash, s.Now()).Error; e != nil {
			return problem(410, "下载凭证已过期")
		}
		if ticket.Released {
			return problem(410, "下载已过期，请重新打开应用下载链接")
		}
		var app Application
		if e = tx.First(&app, "id = ? AND enabled = ? AND blocked = ?", t.AppID, true, false).Error; e != nil {
			return problem(404, "应用已暂停或下架")
		}
		if app.FileKey != ticket.FileKey {
			return problem(410, "应用已更新，请重新打开应用下载链接")
		}
		if ticket.Reserved && !ticket.Charged && ticket.ExpiresAt.Before(s.Now().Add(40*time.Minute)) {
			return tx.Model(&ticket).Update("expires_at", s.Now().Add(40*time.Minute)).Error
		}
		if ticket.Charged || ticket.Reserved {
			return nil
		}
		if ticket.Metered {
			now := s.Now()
			ticket.Reserved = true
			ticket.Period = period(now)
			ticket.StartedAt = &now
			ticket.Size = app.Size
			ticket.AppName = app.Name
			if ticket.ExpiresAt.Before(now.Add(40 * time.Minute)) {
				ticket.ExpiresAt = now.Add(40 * time.Minute)
			}
			return tx.Save(&ticket).Error
		}
		// Tickets issued before the cutover retain their original settlement
		// rules, even though all newly issued tickets use UV billing.
		var p Plan
		e = tx.First(&p, "id = ?", u.PlanID(s.Now())).Error
		if e != nil {
			return e
		}
		settings, e := getSettings(tx)
		if e != nil {
			return e
		}
		usage := Usage{UserID: u.ID, Period: period(s.Now())}
		if e = tx.Where("user_id = ? AND period = ?", u.ID, usage.Period).FirstOrCreate(&usage).Error; e != nil {
			return e
		}
		grant, e := activeDownloadGrant(tx, u, p, s.Now())
		if e != nil {
			return e
		}
		ticket.Free = grant != nil && grant.Used+grant.Reserved < grant.Limit
		if ticket.Free {
			ticket.GrantID = grant.ID
			if e = tx.Model(grant).Update("reserved", gorm.Expr("reserved + 1")).Error; e != nil {
				return e
			}
			usage.FreeReserved++
			if e = tx.Save(&usage).Error; e != nil {
				return e
			}
		} else {
			ticket.PriceMicros = settings.DownloadPriceMicros
			if u.BalanceMicros-u.HeldMicros < ticket.PriceMicros {
				return problem(402, "应用发布者余额不足，暂时无法下载")
			}
			u.HeldMicros += ticket.PriceMicros
			if e = tx.Model(&u).Update("held_micros", u.HeldMicros).Error; e != nil {
				return e
			}
		}
		now := s.Now()
		ticket.Reserved = true
		ticket.Period = usage.Period
		ticket.StartedAt = &now
		ticket.Size = app.Size
		ticket.AppName = app.Name
		if ticket.ExpiresAt.Before(now.Add(40 * time.Minute)) {
			ticket.ExpiresAt = now.Add(40 * time.Minute)
		}
		return tx.Save(&ticket).Error
	})
}
func (s *Server) finishRange(t Ticket, start, end int64) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		u, e := lockUser(tx, t.UserID)
		if e != nil {
			return e
		}
		var ticket Ticket
		if e = tx.First(&ticket, "hash = ?", t.Hash).Error; e != nil {
			return e
		}
		if ticket.Charged {
			return nil
		}
		if !ticket.Reserved || ticket.Released {
			return errors.New("download reservation unavailable")
		}
		if start < 0 || end < start || end >= ticket.Size {
			return errors.New("invalid completed byte range")
		}
		var ranges []byteRange
		if ticket.Ranges != "" {
			if e = json.Unmarshal([]byte(ticket.Ranges), &ranges); e != nil {
				return e
			}
		}
		ranges = mergeRanges(ranges, byteRange{start, end})
		if len(ranges) > 1024 {
			return errors.New("too many disjoint ranges")
		}
		raw, _ := json.Marshal(ranges)
		ticket.Ranges = string(raw)
		if e = s.queueRoutedDownloadStart(tx, ticket); e != nil {
			return e
		}
		if len(ranges) != 1 || ranges[0].Start != 0 || ranges[0].End != ticket.Size-1 {
			return tx.Save(&ticket).Error
		}
		if !ticket.Metered {
			var usage Usage
			if e = tx.First(&usage, "user_id = ? AND period = ?", u.ID, ticket.Period).Error; e != nil {
				return e
			}
			if ticket.Free {
				if usage.FreeReserved < 1 {
					return errors.New("invalid quota reservation")
				}
				if e = settleGiftGrant(tx, ticket, true); e != nil {
					return e
				}
				usage.FreeReserved--
				usage.FreeUsed++
			} else {
				if u.HeldMicros < ticket.PriceMicros {
					return errors.New("invalid balance reservation")
				}
				u.HeldMicros -= ticket.PriceMicros
				if e = tx.Model(&u).Update("held_micros", u.HeldMicros).Error; e != nil {
					return e
				}
				if e = changeBalance(tx, &u, -ticket.PriceMicros, "download:"+ticket.Hash, "download", "成功下载 · "+ticket.AppName); e != nil {
					return e
				}
				usage.PaidUsed++
				usage.SpentMicros += ticket.PriceMicros
			}
			if e = tx.Save(&usage).Error; e != nil {
				return e
			}
		}
		ticket.Charged = true
		if e = tx.Save(&ticket).Error; e != nil {
			return e
		}
		if e = tx.Unscoped().Model(&Application{}).Where("id = ?", ticket.AppID).UpdateColumn("downloads", gorm.Expr("downloads + 1")).Error; e != nil {
			return e
		}
		started := s.Now()
		if ticket.StartedAt != nil {
			started = *ticket.StartedAt
		}
		record := Download{Metered: ticket.Metered, ID: randomHex(16), TicketHash: ticket.Hash, UserID: u.ID, AppID: ticket.AppID, AppName: ticket.AppName, AmountMicros: ticket.PriceMicros, IP: ticket.IP, UserAgent: ticket.UserAgent, Source: ticket.Source, Device: ticket.Device, OS: ticket.OS, Browser: ticket.Browser, Bytes: ticket.Size, AttributionID: ticket.AttributionID, StartedAt: started, CompletedAt: s.Now(), CreatedAt: s.Now()}
		if e = tx.Create(&record).Error; e != nil {
			return e
		}
		return s.queueDownloadConversion(tx, ticket)
	})
}
func (s *Server) releaseTicket(pre Ticket) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		u, e := lockUser(tx, pre.UserID)
		if e != nil {
			return e
		}
		var t Ticket
		if e = tx.First(&t, "hash = ?", pre.Hash).Error; e != nil {
			return e
		}
		if !t.Reserved || t.Charged || t.Released || t.ExpiresAt.After(s.Now()) {
			return nil
		}
		if t.Metered {
			return tx.Model(&t).Update("released", true).Error
		}
		if t.Free {
			if e = settleGiftGrant(tx, t, false); e != nil {
				return e
			}
			res := tx.Model(&Usage{}).Where("user_id = ? AND period = ? AND free_reserved > 0", u.ID, t.Period).UpdateColumn("free_reserved", gorm.Expr("free_reserved - 1"))
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return fmt.Errorf("quota release conflict %s", t.Hash)
			}
		} else {
			if u.HeldMicros < t.PriceMicros {
				return errors.New("balance hold invariant failed")
			}
			if e = tx.Model(&u).Update("held_micros", u.HeldMicros-t.PriceMicros).Error; e != nil {
				return e
			}
		}
		return tx.Model(&t).Update("released", true).Error
	})
}
