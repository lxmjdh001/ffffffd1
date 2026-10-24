package platform

import "net/http"

type downloadFilterApp struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

// Unlike the paginated app-management list, the picker includes every owned
// application and the last known name of hard-deleted apps with download history.
func (s *Server) listDownloadFilterApps(w http.ResponseWriter, r *http.Request) {
	owner := userOf(r).ID
	apps := []downloadFilterApp{}
	err := s.DB.Raw(`
		SELECT id, name, false AS deleted
		FROM applications
		WHERE user_id = ? AND deleted_at IS NULL
		UNION ALL
		SELECT app_id AS id, app_name AS name, true AS deleted
		FROM (
			SELECT d.app_id, d.app_name,
				ROW_NUMBER() OVER (PARTITION BY d.app_id ORDER BY d.created_at DESC, d.id DESC) AS row_number
			FROM downloads d
			WHERE d.user_id = ? AND d.app_id <> '' AND NOT EXISTS (
				SELECT 1 FROM applications a
				WHERE a.id = d.app_id AND a.user_id = ? AND a.deleted_at IS NULL
			)
		) history
		WHERE row_number = 1
		ORDER BY name, id`, owner, owner, owner).Scan(&apps).Error
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]any{"data": apps})
}
