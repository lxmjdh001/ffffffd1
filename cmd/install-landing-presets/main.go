// Install the bundled catalog designs without changing or restarting the server.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"appdock/internal/platform"
)

func main() {
	account := flag.String("admin-account", "", "existing enabled administrator login; required")
	upgrade := flag.Bool("upgrade", false, "upgrade only unchanged original preset versions")
	flag.Parse()
	if *account == "" || flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	config, err := platform.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Invalid server configuration; no templates installed.")
		os.Exit(1)
	}
	db, err := platform.OpenDB(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Database unavailable; no templates installed.")
		os.Exit(1)
	}
	connection, _ := db.DB()
	defer connection.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var result []platform.LandingPresetInstallResult
	if *upgrade {
		result, err = platform.UpgradeLandingPresets(ctx, db, *account)
	} else {
		result, err = platform.InstallLandingPresets(ctx, db, *account)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Template installation failed:", err)
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": true, "templates": result})
}
