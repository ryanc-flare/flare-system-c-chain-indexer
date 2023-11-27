package database

import (
	"flare-ftso-indexer/config"
	"fmt"

	"github.com/go-sql-driver/mysql"
	gormMysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	// List entities to auto-migrate
	entities = []interface{}{
		State{},
		Transaction{},
		Log{},
	}
	HistoryDropIntervalCheck = 60 * 30 // every 30 min
	DBTransactionBatchesSize = 1000
)

func ConnectAndInitialize(cfg *config.DBConfig) (*gorm.DB, error) {
	db, err := Connect(cfg)
	if err != nil {
		return nil, fmt.Errorf("ConnectAndInitialize: Connect: %w", err)
	}

	// Initialize - auto migrate
	err = db.AutoMigrate(entities...)
	if err != nil {
		return nil, fmt.Errorf("ConnectAndInitialize: AutoMigrate %w", err)
	}
	// If the state info is not in the DB, create it
	_, err = GetDBStates(db)
	if err != nil {
		for _, name := range StateNames {
			s := &State{Name: name}
			s.UpdateIndex(0, 0)
			err = db.Create(s).Error
			if err != nil {
				return nil, fmt.Errorf("ConnectAndInitialize: Create: %w", err)
			}
		}
	}

	return db, nil
}

func Connect(cfg *config.DBConfig) (*gorm.DB, error) {
	// Connect to the database
	dbConfig := mysql.Config{
		User:                 cfg.Username,
		Passwd:               cfg.Password,
		Net:                  "tcp",
		Addr:                 fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		DBName:               cfg.Database,
		AllowNativePasswords: true,
		ParseTime:            true,
	}

	var gormLogLevel logger.LogLevel
	if cfg.LogQueries {
		gormLogLevel = logger.Info
	} else {
		gormLogLevel = logger.Silent
	}
	gormConfig := gorm.Config{
		Logger:          logger.Default.LogMode(gormLogLevel),
		CreateBatchSize: DBTransactionBatchesSize,
	}
	return gorm.Open(gormMysql.Open(dbConfig.FormatDSN()), &gormConfig)
}
