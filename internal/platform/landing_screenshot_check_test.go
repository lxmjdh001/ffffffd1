package platform

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestLandingScreenshotCheckIgnoresApplicationCredentialsAndDatabase(t *testing.T) {
	t.Setenv("APP_SECRET", "")
	t.Setenv("DATABASE_URL", "this-database-must-never-be-opened")
	t.Setenv("LANDING_SCREENSHOT_BROWSER", "/nonexistent/d1-screenshot-browser")
	t.Setenv("LANDING_SCREENSHOT_TIMEOUT_SECONDS", "25")
	t.Setenv("LANDING_SCREENSHOT_NO_SANDBOX", "false")
	_, err := CheckLandingScreenshot(context.Background())
	if err == nil || !strings.Contains(err.Error(), "截图浏览器不存在") {
		t.Fatalf("check should only inspect its browser configuration: %v", err)
	}
}

func TestLandingScreenshotCheckRejectsInvalidTimeout(t *testing.T) {
	t.Setenv("LANDING_SCREENSHOT_TIMEOUT_SECONDS", "61")
	t.Setenv("LANDING_SCREENSHOT_NO_SANDBOX", "false")
	if _, err := CheckLandingScreenshot(context.Background()); err == nil || !strings.Contains(err.Error(), "LANDING_SCREENSHOT_TIMEOUT_SECONDS") {
		t.Fatalf("invalid check deadline was accepted: %v", err)
	}
}

func TestLandingScreenshotCheckChromium(t *testing.T) {
	browser := os.Getenv("TEST_LANDING_SCREENSHOT_BROWSER")
	if browser == "" {
		t.Skip("set TEST_LANDING_SCREENSHOT_BROWSER to run native renderer preflight")
	}
	t.Setenv("LANDING_SCREENSHOT_BROWSER", browser)
	t.Setenv("LANDING_SCREENSHOT_TIMEOUT_SECONDS", "25")
	t.Setenv("LANDING_SCREENSHOT_NO_SANDBOX", "false")
	t.Setenv("APP_SECRET", "")
	t.Setenv("DATABASE_URL", "this-database-must-never-be-opened")
	result, err := CheckLandingScreenshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Browser == "" || result.Width != 390 || result.Height != 844 || !result.Sandbox {
		t.Fatalf("unexpected native renderer result: %+v", result)
	}
}
