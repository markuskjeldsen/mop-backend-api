package initializers

import (
	"io"
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func ConnectToDB() {
	var err error

	DB, err = gorm.Open(sqlite.Open("data.db"), &gorm.Config{
		Logger: logger.New(log.New(io.Discard, "", 0), logger.Config{
			LogLevel:                  logger.Silent,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
		}),
	})
	DB.Exec("PRAGMA foreign_keys = ON;")

	if err != nil {
		log.Fatal("failed to connect database")
	}

}
