package dbcore

import (
	"log"
	"sync"

	"github.com/L1ttlebear/ippool/cmd/flags"
	"github.com/L1ttlebear/ippool/config"
	"github.com/L1ttlebear/ippool/database/models"
	logutil "github.com/L1ttlebear/ippool/utils/log"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var (
	instance *gorm.DB
	once     sync.Once
)

func GetDBInstance() *gorm.DB {
	once.Do(func() {
		var err error

		logConfig := &gorm.Config{
			Logger: logutil.NewGormLogger(),
		}

		switch flags.DatabaseType {
		case "sqlite", "":
			instance, err = gorm.Open(sqlite.Open(flags.DatabaseFile), logConfig)
			if err != nil {
				log.Fatalf("Failed to connect to SQLite3 database: %v", err)
			}
			log.Printf("Using SQLite database file: %s", flags.DatabaseFile)
			instance.Exec("PRAGMA wal = ON;")
			instance.Exec("PRAGMA journal_mode = WAL;")
			instance.Exec("VACUUM;")
			instance.Exec("PRAGMA wal_checkpoint(TRUNCATE);")
		default:
			log.Fatalf("Unsupported database type: %s", flags.DatabaseType)
		}

		config.SetDb(instance)

		err = instance.AutoMigrate(
			&models.User{},
			&models.Session{},
			&models.Log{},
			&models.Pool{},
			&models.Host{},
			&models.CheckRecord{},
			&models.HostHeartbeat{},
		)
		if err != nil {
			log.Fatalf("Failed to auto-migrate tables: %v", err)
		}

		ensureDefaultPool(instance)
		syncPoolsFromHosts(instance)
	})
	return instance
}
