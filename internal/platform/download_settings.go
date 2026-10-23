package platform

import (
	"gorm.io/gorm"
	"net/http"
)

func (s *Server) saveDownloadSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mode string `json:"download_mode"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.Mode != "direct" && in.Mode != "info" {
		fail(w, problem(400, "请选择直跳下载或跳转详情页"))
		return
	}
	var a Application
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, userOf(r).ID); err != nil {
			return err
		}
		if err := tx.First(&a, "id = ? AND user_id = ?", r.PathValue("id"), userOf(r).ID).Error; err != nil {
			return err
		}
		if err := tx.Model(&a).Update("download_mode", in.Mode).Error; err != nil {
			return err
		}
		return recordAudit(tx, userOf(r).ID, "app.download_settings", a.ID, in.Mode)
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, a)
}
