package platform

import (
	"gorm.io/gorm"
	"net/http"
	"strings"
	"time"
)

type DownloadDomainNote struct {
	UserID string `json:"-" gorm:"primaryKey;size:32"`
	URL    string `json:"url" gorm:"primaryKey;size:2048"`
	Remark string `json:"remark" gorm:"size:120"`
}
type availableDomain struct {
	URL      string `json:"url"`
	Remark   string `json:"remark"`
	Source   string `json:"source"`
	Audience string `json:"audience"`
	DomainID string `json:"-"`
}

func (s *Server) saveDomainRemark(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Remark string `json:"remark"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	in.Remark = strings.TrimSpace(in.Remark)
	if len([]rune(in.Remark)) > 120 {
		fail(w, problem(400, "域名备注最多 120 字"))
		return
	}
	result := s.DB.Model(&Domain{}).Where("id = ? AND user_id = ?", r.PathValue("id"), userOf(r).ID).Update("remark", in.Remark)
	if result.Error != nil {
		fail(w, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		fail(w, gorm.ErrRecordNotFound)
		return
	}
	jsonOut(w, 200, map[string]string{"remark": in.Remark})
}
func (s *Server) downloadDomainsFor(u User) ([]availableDomain, error) {
	x, err := getSettings(s.DB)
	if err != nil {
		return nil, err
	}
	out := []availableDomain{}
	seen := map[string]bool{}
	add := func(raw, remark, source, audience, id string) {
		normalized, err := s.normalizeDownloadURL(raw)
		if err != nil || normalized == "" || seen[normalized] {
			return
		}
		seen[normalized] = true
		out = append(out, availableDomain{URL: normalized, Remark: remark, Source: source, Audience: audience, DomainID: id})
	}
	plan := u.PlanID(s.Now())
	if len(x.DownloadDomainRules) > 0 {
		for _, rule := range x.DownloadDomainRules {
			if rule.Audience == "all" || rule.Audience == "paid" && plan == "pro" {
				add(rule.URL, rule.Remark, "platform", rule.Audience, "")
			}
		}
	} else {
		add(x.assignedDownloadURL(plan), "", "platform", "all", "")
	}
	var ds []Domain
	if err := s.DB.Where("user_id = ? AND verified_at > ?", u.ID, s.Now().Add(-48*time.Hour)).Order("created_at, id").Find(&ds).Error; err != nil {
		return nil, err
	}
	for _, d := range ds {
		add("https://"+d.Hostname, d.Remark, "custom", "owner", d.ID)
	}
	var notes []DownloadDomainNote
	if err := s.DB.Where("user_id = ?", u.ID).Find(&notes).Error; err != nil {
		return nil, err
	}
	for i := range out {
		for _, note := range notes {
			if note.URL == out[i].URL {
				out[i].Remark = note.Remark
			}
		}
	}
	return out, nil
}
func (s *Server) availableDownloadDomains(w http.ResponseWriter, r *http.Request) {
	ds, err := s.downloadDomainsFor(userOf(r))
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]any{"data": ds})
}
func (s *Server) saveDownloadDomainNote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL    string `json:"url"`
		Remark string `json:"remark"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	in.Remark = strings.TrimSpace(in.Remark)
	if len([]rune(in.Remark)) > 120 {
		fail(w, problem(400, "域名备注最多 120 字"))
		return
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		u, err := lockUser(tx, userOf(r).ID)
		if err != nil {
			return err
		}
		scoped := *s
		scoped.DB = tx
		ds, err := scoped.downloadDomainsFor(u)
		if err != nil {
			return err
		}
		for _, d := range ds {
			if d.URL == in.URL {
				if d.DomainID != "" {
					return tx.Model(&Domain{}).Where("id = ? AND user_id = ?", d.DomainID, u.ID).Update("remark", in.Remark).Error
				}
				return tx.Save(&DownloadDomainNote{UserID: u.ID, URL: d.URL, Remark: in.Remark}).Error
			}
		}
		return problem(403, "无权使用此域名")
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]string{"remark": in.Remark})
}
