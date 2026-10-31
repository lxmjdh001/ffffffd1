package platform

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"time"
)

// This command is deliberately independent of LoadConfig, SQL and workers.
// Operators can verify a candidate download before replacing live databases.
func CheckGeoIP(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("geoip-check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	country := flags.String("country", "", "Country MMDB path")
	asn := flags.String("asn", "", "ASN MMDB path")
	iterations := flags.Int("iterations", 10000, "country lookup timing iterations")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *country == "" || *asn == "" || *iterations < 1 || *iterations > 1000000 {
		return errors.New("geoip-check requires -country, -asn and 1–1000000 iterations")
	}
	db, err := OpenGeoIP(Config{GeoIPCountryDB: *country, GeoIPASNDB: *asn})
	if err != nil {
		return err
	}
	defer db.Close()
	info := db.Info()
	if info.Country == nil || info.ASN == nil {
		return errors.New("geoip-check requires both databases to be enabled")
	}
	ips := flags.Args()
	if len(ips) == 0 {
		ips = []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"}
	}
	type sample struct {
		IP           string `json:"ip"`
		Country      string `json:"country"`
		ASN          uint   `json:"asn"`
		Organization string `json:"organization"`
	}
	results := []sample{}
	addresses := []net.IP{}
	for _, raw := range ips {
		ip := net.ParseIP(raw)
		if _, valid := geoIPAddr(ip); !valid {
			return errors.New("geoip-check sample must be a public IPv4 or IPv6 address")
		}
		number, organization := db.ASN(ip)
		results = append(results, sample{IP: ip.String(), Country: db.Country(ip), ASN: number, Organization: organization})
		addresses = append(addresses, ip)
	}
	start := time.Now()
	for i := 0; i < *iterations; i++ {
		_ = db.Country(addresses[i%len(addresses)])
	}
	return json.NewEncoder(out).Encode(map[string]any{"databases": info, "samples": results, "iterations": *iterations, "country_lookup_mean_ns": time.Since(start).Nanoseconds() / int64(*iterations)})
}
