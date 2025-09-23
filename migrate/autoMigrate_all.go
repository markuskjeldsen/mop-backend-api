package main

import (
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/models"
)

func MigrateTables() {
	initializers.DB.AutoMigrate(
		&models.User{},
		&models.Debitor{},
		&models.Visit{},
		&models.VisitResponse{},
		&models.VisitStatus{},
		&models.VisitStatusLog{},
		&models.VisitResponseImage{},
		&models.LoginAttempt{},
		&models.AuthAttempt{},
		&models.VisitType{},
		&models.ActivityLog{},
	)
}
