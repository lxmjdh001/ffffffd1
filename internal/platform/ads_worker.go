package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Server) RunAdWorker(ctx context.Context) {
	for i := 0; i < 4; i++ {
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					for n := 0; n < 10; n++ {
						if ctx.Err() != nil {
							return
						}
						ok := s.deliverOne(ctx)
						if !ok {
							break
						}
					}
				}
			}
		}()
	}
	<-ctx.Done()
}
func (s *Server) claimDelivery(ctx context.Context) (Delivery, error) {
	var d Delivery
	e := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Where("(status = ? AND next_attempt_at <= ?) OR (status = ? AND lease_until < ?)", "pending", s.Now(), "processing", s.Now()).Order("next_attempt_at ASC")
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		if e := query.First(&d).Error; e != nil {
			return e
		}
		d.Status = "processing"
		d.Attempts++
		d.LeaseUntil = s.Now().Add(2 * time.Minute)
		return tx.Save(&d).Error
	})
	return d, e
}
func (s *Server) deliverOne(ctx context.Context) bool {
	d, e := s.claimDelivery(ctx)
	if e != nil {
		if !errors.Is(e, gorm.ErrRecordNotFound) && ctx.Err() == nil {
			slog.Error("ad queue claim failed", "error", e)
		}
		return false
	}
	requestID, sendErr := s.sendDelivery(ctx, d)
	update := map[string]any{"provider_request_id": requestID, "last_error": ""}
	if sendErr == nil {
		update["status"] = "sent"
		update["sent_at"] = s.Now()
	} else {
		update["last_error"] = cleanText(sendErr.Error(), 500)
		if d.Attempts >= 8 {
			update["status"] = "failed"
		} else {
			update["status"] = "pending"
			delay := time.Minute * time.Duration(1<<min(d.Attempts-1, 6))
			update["next_attempt_at"] = s.Now().Add(delay)
		}
	}
	// Match the lease, so a late worker cannot overwrite a newer claim.
	if e = s.DB.Model(&Delivery{}).Where("id = ? AND status = ? AND lease_until = ?", d.ID, "processing", d.LeaseUntil).Updates(update).Error; e != nil {
		slog.Error("ad result write failed", "delivery", d.ID)
	}
	return true
}
func (s *Server) sendDelivery(ctx context.Context, d Delivery) (string, error) {
	var event Conversion
	if e := s.DB.First(&event, "id = ?", d.ConversionID).Error; e != nil {
		return "", e
	}
	if !event.Consent {
		return "", errors.New("用户未同意广告效果统计")
	}
	if event.EventTime.Before(s.Now().Add(-7 * 24 * time.Hour)) {
		return "", errors.New("事件超过 7 天回传窗口")
	}
	var owner User
	if e := s.DB.First(&owner, "id = ? AND disabled = ?", event.UserID, false).Error; e != nil {
		return "", errors.New("发布者账户已停用")
	}
	var cfg AdConfig
	if e := s.DB.First(&cfg, "app_id = ?", event.AppID).Error; e != nil {
		return "", e
	}
	var attr Attribution
	if e := s.DB.First(&attr, "id = ?", event.AttributionID).Error; e != nil {
		return "", e
	}
	if d.Platform == "facebook" {
		if !cfg.FacebookEnabled || cfg.FacebookPixel != d.PixelID {
			return "", errors.New("Facebook 配置已停用或 Pixel 已变更")
		}
		token, e := s.decryptSecret(cfg.FacebookToken)
		if e != nil {
			return "", e
		}
		user := map[string]any{"client_ip_address": event.IP, "client_user_agent": event.UserAgent}
		if attr.FBC != "" {
			user["fbc"] = attr.FBC
		}
		if attr.FBP != "" {
			user["fbp"] = attr.FBP
		}
		if event.ExternalIDHash != "" {
			user["external_id"] = []string{event.ExternalIDHash}
		}
		// This product distributes website-wrapper APKs. These are WebView web
		// conversions, not native install/MMP events. Native campaigns need an SDK.
		payload := map[string]any{"data": []any{map[string]any{"event_name": d.EventName, "event_id": event.EventID, "event_time": event.EventTime.Unix(), "action_source": "website", "event_source_url": event.SourceURL, "user_data": user}}}
		if cfg.FacebookTestCode != "" {
			payload["test_event_code"] = cfg.FacebookTestCode
		}
		version := s.Config.MetaAPIVersion
		if version == "" {
			version = "v25.0"
		}
		endpoint := "https://graph.facebook.com/" + url.PathEscape(version) + "/" + url.PathEscape(d.PixelID) + "/events"
		status, data, e := s.adRequest(ctx, endpoint, "Authorization", "Bearer "+token, payload)
		if e != nil {
			return "", e
		}
		var result struct {
			EventsReceived int    `json:"events_received"`
			Trace          string `json:"fbtrace_id"`
			Error          *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if e = json.Unmarshal(data, &result); e != nil {
			return "", errors.New("Facebook 响应无效")
		}
		if result.Error != nil {
			return result.Trace, fmt.Errorf("Facebook code=%d: %s", result.Error.Code, strings.ReplaceAll(result.Error.Message, token, "[redacted]"))
		}
		if status != 200 || result.EventsReceived < 1 {
			return result.Trace, fmt.Errorf("Facebook 未接受事件 (HTTP %d)", status)
		}
		return result.Trace, nil
	}
	if !cfg.TikTokEnabled || cfg.TikTokPixel != d.PixelID {
		return "", errors.New("TikTok 配置已停用或 Pixel 已变更")
	}
	token, e := s.decryptSecret(cfg.TikTokToken)
	if e != nil {
		return "", e
	}
	user := map[string]any{"ip": event.IP, "user_agent": event.UserAgent, "ttclid": attr.TTClid}
	if attr.TTP != "" {
		user["ttp"] = attr.TTP
	}
	if event.ExternalIDHash != "" {
		user["external_id"] = event.ExternalIDHash
	}
	payload := map[string]any{"event_source": "web", "event_source_id": d.PixelID, "data": []any{map[string]any{"event": d.EventName, "event_id": event.EventID, "event_time": event.EventTime.Unix(), "user": user, "page": map[string]string{"url": event.SourceURL}}}}
	if cfg.TikTokTestCode != "" {
		payload["test_event_code"] = cfg.TikTokTestCode
	}
	status, data, e := s.adRequest(ctx, "https://business-api.tiktok.com/open_api/v1.3/event/track/", "Access-Token", token, payload)
	if e != nil {
		return "", e
	}
	var result struct {
		Code      *int   `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	if e = json.Unmarshal(data, &result); e != nil || result.Code == nil {
		return "", errors.New("TikTok 响应无效")
	}
	if status != 200 || *result.Code != 0 {
		return result.RequestID, fmt.Errorf("TikTok code=%d: %s", *result.Code, strings.ReplaceAll(result.Message, token, "[redacted]"))
	}
	return result.RequestID, nil
}
func (s *Server) adRequest(ctx context.Context, endpoint, header, token string, payload any) (int, []byte, error) {
	data, e := json.Marshal(payload)
	if e != nil {
		return 0, nil, e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(data))
	if e != nil {
		return 0, nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(header, token)
	resp, e := s.HTTP.Do(req)
	if e != nil {
		return 0, nil, errors.New("广告平台连接失败，稍后自动重试")
	}
	defer resp.Body.Close()
	body, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, body, e
}
