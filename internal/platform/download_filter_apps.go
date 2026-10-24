package platform

import "net/http"

type downloadFilterApp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// The picker includes all current owned apps, never deleted-app snapshots from
// download history. Removing a picker option does not remove historical records.
func (s *Server) listDownloadFilterApps(w http.ResponseWriter, r *http.Request) {
	owner := userOf(r).ID
	apps := []downloadFilterApp{}
	err := s.DB.Raw(`
		SELECT id, name
		FROM applications
		WHERE user_id = ? AND deleted_at IS NULL
		ORDER BY name, id`, owner).Scan(&apps).Error
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]any{"data": apps})
}
