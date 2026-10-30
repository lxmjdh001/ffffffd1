// migrate applies transactional schema migrations without bootstrapping users,
// starting workers, publishing assets, or listening for HTTP requests.
package main

import (
	"appdock/internal/platform"
	"encoding/json"
	"os"
)

func main() {
	c, e := platform.LoadConfig()
	if e == nil {
		var dbErr error
		db, err := platform.OpenDB(c)
		if err != nil {
			e = err
		} else {
			dbErr = platform.Migrate(db)
			sql, _ := db.DB()
			_ = sql.Close()
			e = dbErr
		}
	}
	if e != nil {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "failed"})
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "passed", "transaction_committed": true})
}
