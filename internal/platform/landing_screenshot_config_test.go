package platform

import (
	"testing"
	"time"
)

func TestLandingScreenshotConfigDefaults(t *testing.T) {
	t.Setenv("LANDING_SCREENSHOT_BROWSER", "")
	t.Setenv("LANDING_SCREENSHOT_NO_SANDBOX", "")
	t.Setenv("LANDING_SCREENSHOT_TIMEOUT_SECONDS", "")
	var cfg Config
	if err := cfg.loadLandingScreenshotConfig(); err != nil {
		t.Fatal(err)
	}
	if cfg.LandingScreenshotBrowser != "" || cfg.LandingScreenshotNoSandbox || cfg.LandingScreenshotTimeout != 25*time.Second {
		t.Fatalf("unexpected screenshot defaults: browser=%q noSandbox=%t timeout=%s", cfg.LandingScreenshotBrowser, cfg.LandingScreenshotNoSandbox, cfg.LandingScreenshotTimeout)
	}
}

func TestLandingScreenshotConfigOverrides(t *testing.T) {
	t.Setenv("LANDING_SCREENSHOT_BROWSER", " /opt/chromium ")
	t.Setenv("LANDING_SCREENSHOT_NO_SANDBOX", "true")
	for _, seconds := range []string{"10", "60"} {
		t.Setenv("LANDING_SCREENSHOT_TIMEOUT_SECONDS", seconds)
		var cfg Config
		if err := cfg.loadLandingScreenshotConfig(); err != nil {
			t.Fatal(err)
		}
		if cfg.LandingScreenshotBrowser != "/opt/chromium" || !cfg.LandingScreenshotNoSandbox {
			t.Fatal("explicit screenshot configuration was not preserved")
		}
		if cfg.LandingScreenshotTimeout < 10*time.Second || cfg.LandingScreenshotTimeout > 60*time.Second {
			t.Fatal("valid timeout boundary was not accepted")
		}
	}
}

func TestLandingScreenshotConfigRejectsInvalidSettings(t *testing.T) {
	t.Setenv("LANDING_SCREENSHOT_NO_SANDBOX", "false")
	for _, seconds := range []string{"9", "61", "0", "-1", "25.5", "unlimited"} {
		t.Run(seconds, func(t *testing.T) {
			t.Setenv("LANDING_SCREENSHOT_TIMEOUT_SECONDS", seconds)
			var cfg Config
			if err := cfg.loadLandingScreenshotConfig(); err == nil {
				t.Fatalf("invalid timeout %q was accepted", seconds)
			}
		})
	}
	t.Setenv("LANDING_SCREENSHOT_TIMEOUT_SECONDS", "25")
	t.Setenv("LANDING_SCREENSHOT_NO_SANDBOX", "maybe")
	var cfg Config
	if err := cfg.loadLandingScreenshotConfig(); err == nil {
		t.Fatal("invalid sandbox setting was accepted")
	}
}
