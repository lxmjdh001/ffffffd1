// Package d1sdk signs real customer-server events. Store the API secret only on
// your server and call this after your own registration transaction commits.
package d1sdk

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Event struct {
	AttributionToken string `json:"attribution_token"`
	Event            string `json:"event"`
	EventID          string `json:"event_id"`
	EventTime        int64  `json:"event_time"`
	Consent          bool   `json:"consent"`
	PageURL          string `json:"page_url"`
	ExternalID       string `json:"external_id,omitempty"`
	ClientIP         string `json:"client_ip,omitempty"`
	UserAgent        string `json:"user_agent,omitempty"`
}

func Send(ctx context.Context, client *http.Client, baseURL, appID, secret string, event Event) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	ts := fmt.Sprint(time.Now().Unix())
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = io.WriteString(mac, ts+"\n")
	_, _ = mac.Write(body)
	r, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(baseURL, "/")+"/api/sdk/apps/"+appID+"/events", bytes.NewReader(body))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-D1-Time", ts)
	r.Header.Set("X-D1-Signature", hex.EncodeToString(mac.Sum(nil)))
	response, err := client.Do(r)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		// Do not log the signed request body: it contains an attribution token.
		return fmt.Errorf("D1 Apk HTTP %d; retry 409/429/5xx using the SAME EventID and EventTime", response.StatusCode)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 65536))
	return nil
}
