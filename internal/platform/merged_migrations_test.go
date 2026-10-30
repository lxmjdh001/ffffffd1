package platform

import "testing"

func TestMergedMigrationHistories(t *testing.T) {
	for _, history := range []string{"main-user-deletion", "production-attribution"} {
		t.Run(history, func(t *testing.T) {
			s := testServer(t)
			u := seedUser(t, s, 123*Dollar)
			if err := s.DB.Delete(&Migration{}, "version = ?", userDeletionMigrationVersion).Error; err != nil {
				t.Fatal(err)
			}
			if history == "main-user-deletion" {
				// Main had already recorded version 30, without attribution fields.
				for _, column := range []struct {
					model any
					name  string
				}{{&Attribution{}, "SourceAttributionID"}, {&Conversion{}, "DownloadedAppID"}} {
					if err := s.DB.Migrator().DropColumn(column.model, column.name); err != nil {
						t.Fatal(err)
					}
				}
			} else if err := s.DB.Migrator().DropColumn(&User{}, "DeletedAt"); err != nil {
				// Production version 30 had attribution, without user tombstones.
				t.Fatal(err)
			}
			for range 2 {
				if err := Migrate(s.DB); err != nil {
					t.Fatal(err)
				}
			}
			for _, column := range []struct {
				model any
				name  string
			}{{&User{}, "DeletedAt"}, {&Attribution{}, "SourceAttributionID"}, {&Conversion{}, "DownloadedAppID"}, {&Cloak{}, "AllowedCountries"}} {
				if !s.DB.Migrator().HasColumn(column.model, column.name) {
					t.Fatalf("missing %s", column.name)
				}
			}
			var stored User
			if err := s.DB.First(&stored, "id = ?", u.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.BalanceMicros != u.BalanceMicros || stored.DeletedAt != nil || stored.Disabled {
				t.Fatal("migration changed the existing user")
			}
		})
	}
}
