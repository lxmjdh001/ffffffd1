package platform

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// These are operator-supplied networks, not automatic ASN/provider judgements.
//
//go:embed data/datacenter-networks.txt
var initialDatacenterNetworks string

const datacenterNetworkLimit = 20000

type DatacenterNetwork struct {
	ID        string    `json:"id" gorm:"primaryKey;size:32"`
	CIDR      string    `json:"cidr" gorm:"column:cidr;uniqueIndex;size:50;not null"`
	Remark    string    `json:"remark" gorm:"size:200;not null;default:''"`
	Enabled   bool      `json:"enabled" gorm:"not null;default:false;index"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func normalizeDatacenterNetwork(raw string) (netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if address, err := netip.ParseAddr(raw); err == nil && address.Zone() == "" {
		address = address.Unmap()
		return netip.PrefixFrom(address, address.BitLen()), nil
	}
	prefix, err := netip.ParsePrefix(raw)
	if err != nil || prefix.Addr().Zone() != "" {
		return netip.Prefix{}, problem(400, "请输入有效的 IPv4、IPv6 或 CIDR 网段")
	}
	if prefix.Addr().Is4In6() {
		if prefix.Bits() < 96 {
			return netip.Prefix{}, problem(400, "IPv4 映射地址的掩码须至少为 96")
		}
		prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
	}
	return prefix.Masked(), nil
}

func parseDatacenterNetworks(raw string) ([]netip.Prefix, int, error) {
	var out []netip.Prefix
	seen := map[netip.Prefix]bool{}
	duplicates, count := 0, 0
	for line, value := range strings.Split(raw, "\n") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		count++
		if count > 1000 {
			return nil, 0, problem(400, "每次最多导入 1000 条 IP 或 CIDR")
		}
		prefix, err := normalizeDatacenterNetwork(value)
		if err != nil {
			return nil, 0, problem(400, fmt.Sprintf("第 %d 行：%s", line+1, err.Error()))
		}
		if seen[prefix] {
			duplicates++
			continue
		}
		seen[prefix] = true
		out = append(out, prefix)
	}
	if len(out) == 0 {
		return nil, 0, problem(400, "请至少填写一条 IP 或 CIDR")
	}
	return out, duplicates, nil
}

func migrateDatacenterNetworks(tx *gorm.DB) error {
	var applied int64
	if err := tx.Model(&Migration{}).Where("version = ?", 35).Count(&applied).Error; err != nil {
		return err
	}
	if applied != 0 {
		return nil
	}
	if err := tx.AutoMigrate(&DatacenterNetwork{}); err != nil {
		return err
	}
	prefixes, _, err := parseDatacenterNetworks(initialDatacenterNetworks)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	rows := make([]DatacenterNetwork, 0, len(prefixes))
	for _, prefix := range prefixes {
		rows = append(rows, DatacenterNetwork{ID: randomHex(16), CIDR: prefix.String(), Remark: "初始名单", Enabled: true, CreatedAt: now, UpdatedAt: now})
	}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "cidr"}}, DoNothing: true}).CreateInBatches(rows, 200).Error; err != nil {
		return err
	}
	// Seed once: an administrator's later deletions or edits survive restarts.
	return tx.Create(&Migration{Version: 35, AppliedAt: now}).Error
}

type datacenterNetworkCache struct {
	mu       sync.Mutex
	prefixes []netip.Prefix
	expires  time.Time
}

func (s *Server) invalidateDatacenterNetworks() {
	s.datacenterNetworks.mu.Lock()
	defer s.datacenterNetworks.mu.Unlock()
	s.datacenterNetworks.expires = time.Time{}
}
func (s *Server) datacenterIPBlocked(ctx context.Context, raw string) (bool, error) {
	address, err := netip.ParseAddr(raw)
	if err != nil || address.Zone() != "" {
		return false, nil
	}
	address = address.Unmap()
	cache := &s.datacenterNetworks
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if !s.Now().Before(cache.expires) {
		var rows []DatacenterNetwork
		if err := s.DB.WithContext(ctx).Select("cidr").Where("enabled = ?", true).Limit(datacenterNetworkLimit + 1).Find(&rows).Error; err != nil {
			return false, err
		}
		if len(rows) > datacenterNetworkLimit {
			return false, errors.New("datacenter rule limit exceeded")
		}
		prefixes := make([]netip.Prefix, 0, len(rows))
		for _, row := range rows {
			prefix, err := normalizeDatacenterNetwork(row.CIDR)
			if err != nil {
				return false, err
			}
			prefixes = append(prefixes, prefix)
		}
		cache.prefixes, cache.expires = prefixes, s.Now().Add(5*time.Second)
	}
	for _, prefix := range cache.prefixes {
		if prefix.Contains(address) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) adminDatacenterNetworks(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(query) > 200 {
		fail(w, problem(400, "搜索内容最多 200 字"))
		return
	}
	q := s.DB.Model(&DatacenterNetwork{})
	switch r.URL.Query().Get("enabled") {
	case "true":
		q = q.Where("enabled = ?", true)
	case "false":
		q = q.Where("enabled = ?", false)
	case "", "all":
	default:
		fail(w, problem(400, "请选择有效的状态"))
		return
	}
	switch r.URL.Query().Get("family") {
	case "ipv4":
		q = q.Where("cidr NOT LIKE ?", "%:%")
	case "ipv6":
		q = q.Where("cidr LIKE ?", "%:%")
	case "", "all":
	default:
		fail(w, problem(400, "请选择 IPv4 或 IPv6"))
		return
	}
	var rows []DatacenterNetwork
	// The bounded operator catalogue also supports membership searches by IP.
	if err := q.Order("created_at DESC, id ASC").Limit(datacenterNetworkLimit + 1).Find(&rows).Error; err != nil {
		fail(w, err)
		return
	}
	if len(rows) > datacenterNetworkLimit {
		fail(w, problem(503, "IP 规则数量超出上限"))
		return
	}
	visible := make([]DatacenterNetwork, 0, len(rows))
	address, ipErr := netip.ParseAddr(query)
	for _, row := range rows {
		matched := query == "" || strings.Contains(strings.ToLower(row.CIDR), strings.ToLower(query)) || strings.Contains(strings.ToLower(row.Remark), strings.ToLower(query))
		if ipErr == nil && address.Zone() == "" {
			if prefix, err := normalizeDatacenterNetwork(row.CIDR); err == nil && prefix.Contains(address.Unmap()) {
				matched = true
			}
		}
		if matched {
			visible = append(visible, row)
		}
	}
	var total, enabled int64
	if err := s.DB.Model(&DatacenterNetwork{}).Count(&total).Error; err != nil {
		fail(w, err)
		return
	}
	if err := s.DB.Model(&DatacenterNetwork{}).Where("enabled = ?", true).Count(&enabled).Error; err != nil {
		fail(w, err)
		return
	}
	offset, limit := pageParams(r)
	end := min(len(visible), offset+limit)
	start := min(len(visible), offset)
	jsonOut(w, 200, map[string]any{"data": visible[start:end], "total": len(visible), "rules_total": total, "enabled_total": enabled})
}

type datacenterNetworkInput struct {
	CIDR    *string `json:"cidr"`
	Remark  *string `json:"remark"`
	Enabled *bool   `json:"enabled"`
}

func lockDatacenterCatalogue(tx *gorm.DB) error {
	if tx.Dialector.Name() == "postgres" {
		return tx.Exec("SELECT pg_advisory_xact_lock(681247035)").Error
	}
	return nil
}
func (s *Server) saveDatacenterNetwork(w http.ResponseWriter, r *http.Request) {
	var in datacenterNetworkInput
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	var prefix netip.Prefix
	if in.CIDR != nil {
		var err error
		prefix, err = normalizeDatacenterNetwork(*in.CIDR)
		if err != nil {
			fail(w, err)
			return
		}
	}
	if (r.Method == http.MethodPost || r.Method == http.MethodPut) && in.CIDR == nil {
		fail(w, problem(400, "请填写 IP 或 CIDR"))
		return
	}
	if in.CIDR == nil && in.Remark == nil && in.Enabled == nil {
		fail(w, problem(400, "请填写需要修改的规则"))
		return
	}
	if in.Remark != nil && utf8.RuneCountInString(*in.Remark) > 200 {
		fail(w, problem(400, "备注最多 200 字"))
		return
	}
	var row DatacenterNetwork
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockDatacenterCatalogue(tx); err != nil {
			return err
		}
		action := "datacenter.create"
		if id := r.PathValue("id"); id != "" {
			if err := tx.First(&row, "id = ?", id).Error; err != nil {
				return err
			}
			action = "datacenter.update"
		} else {
			var count int64
			if err := tx.Model(&DatacenterNetwork{}).Count(&count).Error; err != nil {
				return err
			}
			if count >= datacenterNetworkLimit {
				return problem(400, "最多可保存 20000 条 IP 规则")
			}
			row = DatacenterNetwork{ID: randomHex(16), Enabled: true, CreatedAt: s.Now()}
		}
		if in.CIDR != nil {
			row.CIDR = prefix.String()
		}
		if in.Remark != nil {
			row.Remark = strings.TrimSpace(cleanText(*in.Remark, 800))
		}
		if in.Enabled != nil {
			row.Enabled = *in.Enabled
		}
		row.UpdatedAt = s.Now()
		if err := tx.Save(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return problem(409, "该 IP 或网段已存在，请编辑已有规则")
			}
			return err
		}
		return recordAudit(tx, userOf(r).ID, action, row.ID, row.CIDR)
	})
	if err != nil {
		fail(w, err)
		return
	}
	s.invalidateDatacenterNetworks()
	jsonOut(w, 200, row)
}
func (s *Server) importDatacenterNetworks(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Entries string `json:"entries"`
		Remark  string `json:"remark"`
		Enabled *bool  `json:"enabled"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if utf8.RuneCountInString(in.Remark) > 200 {
		fail(w, problem(400, "备注最多 200 字"))
		return
	}
	prefixes, duplicates, err := parseDatacenterNetworks(in.Entries)
	if err != nil {
		fail(w, err)
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	inserted := int64(0)
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockDatacenterCatalogue(tx); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&DatacenterNetwork{}).Count(&count).Error; err != nil {
			return err
		}
		for _, prefix := range prefixes {
			row := DatacenterNetwork{ID: randomHex(16), CIDR: prefix.String(), Remark: strings.TrimSpace(cleanText(in.Remark, 800)), Enabled: enabled, CreatedAt: s.Now(), UpdatedAt: s.Now()}
			result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "cidr"}}, DoNothing: true}).Create(&row)
			if result.Error != nil {
				return result.Error
			}
			inserted += result.RowsAffected
			if count+inserted > datacenterNetworkLimit {
				return problem(400, "最多可保存 20000 条 IP 规则")
			}
		}
		return recordAudit(tx, userOf(r).ID, "datacenter.import", "", fmt.Sprintf("新增 %d 条，跳过 %d 条重复规则", inserted, duplicates+len(prefixes)-int(inserted)))
	})
	if err != nil {
		fail(w, err)
		return
	}
	s.invalidateDatacenterNetworks()
	jsonOut(w, 200, map[string]any{"inserted": inserted, "duplicates": duplicates + len(prefixes) - int(inserted)})
}
func (s *Server) deleteDatacenterNetwork(w http.ResponseWriter, r *http.Request) {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockDatacenterCatalogue(tx); err != nil {
			return err
		}
		var row DatacenterNetwork
		if err := tx.First(&row, "id = ?", r.PathValue("id")).Error; err != nil {
			return err
		}
		if err := tx.Delete(&row).Error; err != nil {
			return err
		}
		return recordAudit(tx, userOf(r).ID, "datacenter.delete", row.ID, row.CIDR)
	})
	if err != nil {
		fail(w, err)
		return
	}
	s.invalidateDatacenterNetworks()
	jsonOut(w, 200, map[string]bool{"ok": true})
}
