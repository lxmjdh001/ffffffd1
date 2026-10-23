package platform

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type Config struct {
	Role, RemoteStorageURL                                                string
	S3Endpoint, S3AccessKey, S3SecretKey, S3Bucket, S3Region              string
	S3Secure                                                              bool
	MetaAPIVersion                                                        string
	Env, Addr, DatabaseURL, DataDir, WebDir, PublicURL, Secret            string
	CNAMEHost                                                             string
	AdminEmail, AdminPassword                                             string
	ClamAVAddr                                                            string
	FutoonURL, FutoonPID, FutoonKey                                       string
	USDTMonitorURL, USDTMonitorSecret, USDTWallet, TronAPIURL, TronAPIKey string
	USDTProvider, USDTAddressPasswordHash                                 string
	USDTMonitorAllowHTTP                                                  bool
	TrustedProxies                                                        []*net.IPNet
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func LoadConfig() (Config, error) {
	c := Config{Env: env("APP_ENV", "development"), Addr: env("HTTP_ADDR", "127.0.0.1:8088"), DatabaseURL: os.Getenv("DATABASE_URL"), DataDir: env("DATA_DIR", "data"), WebDir: env("WEB_DIR", "web/dist"), PublicURL: strings.TrimRight(env("APP_URL", "http://localhost:8088"), "/"), Secret: os.Getenv("APP_SECRET"), AdminEmail: env("ADMIN_ACCOUNT", os.Getenv("ADMIN_EMAIL")), AdminPassword: os.Getenv("ADMIN_PASSWORD"), ClamAVAddr: os.Getenv("CLAMAV_ADDR"), FutoonURL: os.Getenv("FUTOON_API_URL"), FutoonPID: os.Getenv("FUTOON_PID"), FutoonKey: os.Getenv("FUTOON_KEY"), USDTMonitorURL: os.Getenv("USDT_MONITOR_URL"), USDTMonitorSecret: os.Getenv("USDT_MONITOR_SECRET"), USDTWallet: os.Getenv("USDT_WALLET"), TronAPIURL: env("TRON_API_URL", "https://api.trongrid.io"), TronAPIKey: os.Getenv("TRON_API_KEY")}
	c.Role = env("SERVER_ROLE", "all")
	c.USDTProvider = env("USDT_PROVIDER", "tron")
	c.USDTAddressPasswordHash = os.Getenv("USDT_ADDRESS_PASSWORD_HASH")
	c.USDTMonitorAllowHTTP = env("USDT_MONITOR_ALLOW_HTTP", "false") == "true"
	c.RemoteStorageURL = os.Getenv("REMOTE_STORAGE_URL")
	c.S3Endpoint = os.Getenv("S3_ENDPOINT")
	c.S3AccessKey = os.Getenv("S3_ACCESS_KEY")
	c.S3SecretKey = os.Getenv("S3_SECRET_KEY")
	c.S3Bucket = env("S3_BUCKET", "d1apk")
	c.S3Region = env("S3_REGION", "us-east-1")
	c.S3Secure = env("S3_SECURE", "true") == "true"
	c.MetaAPIVersion = env("META_API_VERSION", "v25.0")
	c.CNAMEHost = env("CNAME_TARGET", "dns.d1apk.com")
	for _, v := range strings.Split(os.Getenv("TRUSTED_PROXY_CIDRS"), ",") {
		if strings.TrimSpace(v) == "" {
			continue
		}
		_, cidr, err := net.ParseCIDR(strings.TrimSpace(v))
		if err != nil {
			return c, errors.New("invalid TRUSTED_PROXY_CIDRS")
		}
		c.TrustedProxies = append(c.TrustedProxies, cidr)
	}
	// Accept go7's environment names as aliases so both applications can use
	// the same gateway credentials without sharing their callback URLs.
	c.USDTMonitorURL = env("USDT_MONITOR_URL", os.Getenv("USDT_API_URL"))
	c.USDTMonitorSecret = env("USDT_MONITOR_SECRET", os.Getenv("USDT_API_SECRET"))
	c.USDTWallet = env("USDT_WALLET", os.Getenv("USDT_WALLET_ADDRESS"))
	if c.FutoonURL != "" {
		gateway, err := url.Parse(c.FutoonURL)
		if err == nil && (gateway.Path == "" || gateway.Path == "/") {
			gateway.Path = "/submit.php"
			c.FutoonURL = gateway.String()
		}
	}
	return c, c.Validate()
}

func (c Config) paymentMethods() []string {
	methods := []string{}
	if c.USDTWallet != "" && (c.usdtProvider() != "uzf" || (c.USDTMonitorURL != "" && c.USDTMonitorSecret != "")) {
		methods = append(methods, "usdt")
	}
	return append(methods, "manual")
}

func (c Config) usdtProvider() string {
	if c.USDTProvider == "" {
		return "tron"
	}
	return c.USDTProvider
}

func (c Config) receivingWallet(settings Settings) string {
	if settings.USDTWallet != nil {
		return *settings.USDTWallet
	}
	return c.USDTWallet
}
func (c Config) Production() bool { return c.Env == "production" }
func (c Config) Host() string {
	u, _ := url.Parse(c.PublicURL)
	if u == nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
func (c Config) Validate() error {
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("APP_URL must be a canonical origin, without a path")
	}
	if len(c.Secret) < 32 {
		return errors.New("APP_SECRET must contain at least 32 random characters; run scripts/dev.sh for local setup")
	}
	if c.Env != "production" && c.Env != "development" && c.Env != "test" {
		return errors.New("invalid APP_ENV")
	}
	if c.Role != "all" && c.Role != "control" && c.Role != "download" && c.Role != "" {
		return errors.New("invalid SERVER_ROLE")
	}
	if c.RemoteStorageURL != "" && (c.Role != "control" || c.S3Endpoint != "") {
		return errors.New("remote storage requires SERVER_ROLE=control and no S3 endpoint")
	}
	if c.S3Endpoint != "" && (c.S3AccessKey == "" || c.S3SecretKey == "" || c.S3Bucket == "") {
		return errors.New("incomplete S3 configuration")
	}
	if c.Production() && (u.Scheme != "https" || c.DatabaseURL == "" || (c.Role != "download" && c.ClamAVAddr == "")) {
		return errors.New("production requires HTTPS APP_URL, PostgreSQL DATABASE_URL and CLAMAV_ADDR")
	}
	if (c.FutoonURL != "" || c.FutoonPID != "" || c.FutoonKey != "") && (c.FutoonURL == "" || c.FutoonPID == "" || c.FutoonKey == "") {
		return errors.New("all FUTOON settings are required together")
	}
	if (c.USDTMonitorURL == "") != (c.USDTMonitorSecret == "") {
		return errors.New("USDT_MONITOR_URL and USDT_MONITOR_SECRET must be set together")
	}
	if c.usdtProvider() != "tron" && c.usdtProvider() != "uzf" {
		return errors.New("USDT_PROVIDER must be tron or uzf")
	}
	if c.USDTAddressPasswordHash != "" {
		if _, err := bcrypt.Cost([]byte(c.USDTAddressPasswordHash)); err != nil {
			return errors.New("USDT_ADDRESS_PASSWORD_HASH must be a valid bcrypt hash")
		}
	}
	for i, raw := range []string{c.FutoonURL, c.USDTMonitorURL, c.TronAPIURL} {
		if raw == "" {
			continue
		}
		p, e := url.Parse(raw)
		loopbackMonitor := false
		if e == nil && i == 1 {
			ip := net.ParseIP(p.Hostname())
			loopbackMonitor = ip != nil && ip.IsLoopback() && p.Scheme == "http"
		}
		// The operator may explicitly accept an existing UZF HTTP endpoint.
		// This exception never relaxes other payment endpoints or redirects.
		legacyHTTPMonitor := e == nil && i == 1 && c.usdtProvider() == "uzf" && c.USDTMonitorAllowHTTP && p.Scheme == "http"
		if e != nil || p.Host == "" || p.User != nil || p.RawQuery != "" || p.Fragment != "" || (!loopbackMonitor && !legacyHTTPMonitor && p.Scheme != "https" && (c.Production() || p.Scheme != "http")) {
			return errors.New("payment endpoints must be HTTPS URLs without query, fragment or credentials")
		}
	}
	if c.USDTWallet != "" && !validTronAddress(c.USDTWallet) {
		return errors.New("USDT_WALLET must be a valid mainnet TRON address")
	}
	return nil
}

var billingZone = time.FixedZone("Asia/Shanghai", 8*60*60)

func period(t time.Time) string { return t.In(billingZone).Format("2006-01") }

// Clamp the anniversary to the last day, so Jan 31 renews to Feb 28/29.
func nextMonth(t time.Time) time.Time {
	x := t.In(billingZone)
	y, m, d := x.Date()
	first := time.Date(y, m+1, 1, x.Hour(), x.Minute(), x.Second(), x.Nanosecond(), billingZone)
	last := first.AddDate(0, 1, -1).Day()
	if d > last {
		d = last
	}
	return first.AddDate(0, 0, d-1).UTC()
}
