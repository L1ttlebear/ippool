package dbcore

import (
	"log"
	"strings"

	"github.com/L1ttlebear/ippool/database/models"
	"gorm.io/gorm"
)

func ensureDefaultPool(db *gorm.DB) {
	var cnt int64
	if err := db.Model(&models.Pool{}).Where("name = ?", "default").Count(&cnt).Error; err != nil {
		log.Printf("check default pool failed: %v", err)
		return
	}
	if cnt > 0 {
		return
	}
	if err := db.Create(&models.Pool{Name: "default"}).Error; err != nil {
		log.Printf("create default pool failed: %v", err)
	}
}

func syncPoolsFromHosts(db *gorm.DB) {
	var names []string
	if err := db.Model(&models.Host{}).Distinct("pool").Pluck("pool", &names).Error; err != nil {
		log.Printf("sync pools from hosts failed: %v", err)
		return
	}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		var cnt int64
		if err := db.Model(&models.Pool{}).Where("name = ?", n).Count(&cnt).Error; err != nil {
			continue
		}
		if cnt == 0 {
			_ = db.Create(&models.Pool{Name: n}).Error
		}
	}
}
