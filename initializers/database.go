package initializers

import (
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var DB *gorm.DB

func ConnectToDB() {
	var err error

	DB, err = gorm.Open(sqlite.Open("data.db"), &gorm.Config{})
	DB.Exec("PRAGMA foreign_keys = ON;")

	if err != nil {
		log.Fatal("failed to connect database")
	}

}
