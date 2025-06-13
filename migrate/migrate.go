package main

import (
	"time"

	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/models"
	"golang.org/x/crypto/bcrypt"
)

func init() {
	initializers.LoadEnvVariables()
	initializers.ConnectToDB()
}

func main() {
	initializers.DB.Exec("DROP TABLE IF EXISTS users;")
	initializers.DB.Exec("DROP TABLE IF EXISTS visits;")
	initializers.DB.Exec("DROP TABLE IF EXISTS visit_responses")

	initializers.DB.AutoMigrate(&models.User{})          // Migrate the schema
	initializers.DB.AutoMigrate(&models.Visit{})         // Migrate the schema
	initializers.DB.AutoMigrate(&models.VisitResponse{}) // Migrate the schema

	//Hash the password

	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(user.Password), 14)
	hashedPassword1, _ := bcrypt.GenerateFromPassword([]byte(user1.Password), 14)

	user.Password = string(hashedPassword)
	user1.Password = string(hashedPassword1)
	initializers.DB.Create(&user)  // Save the user to the database
	initializers.DB.Create(&user1) // Save the user to the database

	visit1.UserID = user.ID
	visit2.UserID = user.ID
	initializers.DB.Create(&visit1) // Save the visit to the database
	initializers.DB.Create(&visit2) // Save the visit to the database

	visitResponse1.UserID = user.ID
	visitResponse2.UserID = user.ID
	visitResponse1.VisitID = visit1.ID
	visitResponse2.VisitID = visit2.ID
	initializers.DB.Create(&visitResponse1) // Save the visit response to the database
	initializers.DB.Create(&visitResponse2) // Save the visit response to the database

	visit3.UserID = user1.ID
	visit4.UserID = user1.ID
	visit5.UserID = user1.ID
	visit6.UserID = user1.ID
	initializers.DB.Create(&visit3) // Save the visit to the database
	initializers.DB.Create(&visit4) // Save the visit to the database
	initializers.DB.Create(&visit5) // Save the visit to the database
	initializers.DB.Create(&visit6)

	visitResponse3.UserID = user1.ID
	visitResponse4.UserID = user1.ID
	visitResponse3.VisitID = visit3.ID
	visitResponse4.VisitID = visit4.ID
	initializers.DB.Create(&visitResponse3) // Save the visit response to the database
	initializers.DB.Create(&visitResponse4) // Save the visit response to the database

}

// placeholder information
var user = models.User{
	Username: "markus",
	Password: "pass",
	Email:    "Markus@kjeldsen.dk",
	Phone:    "42480991",
	Rights:   models.RightsDeveloper,
}
var user1 = models.User{
	Username: "patrick",
	Password: "pass",
	Email:    "Patrick@olsen.dk",
	Phone:    "21193038",
	Rights:   models.RightsUser,
}
var visit1 = models.Visit{
	UserID:        user.ID,
	Address:       "123 Main St",
	DebitorName:   "Grinch",
	VisitInterval: "10:00-13:00",
	Latitude:      "37.7749",
	Longitude:     "-122.4194",
	Notes:         "First visit",
	Sagsnr:        1,
	VistDate:      time.Now(),
	VisitTime:     "10:00 AM",
}
var visit2 = models.Visit{
	UserID:        user.ID,
	Address:       "123 Main St",
	DebitorName:   "Grinch",
	VisitInterval: "10:00-13:00",
	Latitude:      "37.7749",
	Longitude:     "-122.4194",
	Notes:         "First visit",
	Sagsnr:        2,
	VistDate:      time.Now(),
	VisitTime:     "12:00 AM",
}
var visit3 = models.Visit{
	UserID:        user.ID,
	Address:       "1337 Main St",
	DebitorName:   "Grinch",
	VisitInterval: "10:00-13:00",
	Latitude:      "37.7749",
	Longitude:     "2.4194",
	Notes:         "First visit",
	Sagsnr:        3,
	VistDate:      time.Now(),
	VisitTime:     "12:00 AM",
}
var visit4 = models.Visit{
	UserID:        user.ID,
	Address:       "1337 Main St",
	DebitorName:   "Grinch",
	VisitInterval: "10:00-13:00",
	Latitude:      "37.7749",
	Longitude:     "2.4194",
	Notes:         "First visit",
	Sagsnr:        4,
	VistDate:      time.Now().AddDate(0, 0, 2),
	VisitTime:     "18:00 AM",
}
var visit5 = models.Visit{
	UserID:        user.ID,
	Address:       "1337 Main St",
	DebitorName:   "Grinch",
	VisitInterval: "10:00-13:00",
	Latitude:      "37.7749",
	Longitude:     "2.4194",
	Notes:         "First visit",
	Sagsnr:        4,
	VistDate:      time.Now().AddDate(0, 0, -1),
	VisitTime:     "18:00 AM",
	Visited:       true,
}

var visit6 = models.Visit{
	UserID:        user.ID,
	Address:       "1337 Main St",
	DebitorName:   "Grinch",
	VisitInterval: "10:00-13:00",
	Latitude:      "37.7749",
	Longitude:     "2.4194",
	Notes:         "First visit",
	Sagsnr:        4,
	VistDate:      time.Now().AddDate(0, 0, -1),
	VisitTime:     "18:00 AM",
	Visited:       false,
}

var visitResponse1 = models.VisitResponse{
	UserID:  user.ID,
	VisitID: visit2.ID,
	ActDate: time.Now(),
	ActTime: "10:00 AM",
	ActLat:  "37.7749",
	ActLong: "-122.4194",
	// Response data
	DebitorIsHome: true,

	AssetAtAddress: true,
	AssetDamaged:   false,

	CivilStatus:     models.Cohabiting,
	ChildrenUnder18: 10,
	ChildrenOver18:  10,
	ChildSupport:    4000,

	HasWork:  true,
	Position: "CEO",
	Salary:   50000,

	Creditor:   "nordania",
	DebtAmount: 1000000,
	Settlement: "forlig",

	PropertyType:      models.Apartment,
	MaintenanceStatus: models.Deteriorated,

	OwnershipStatus: "renter",

	Comments: "Meget grimt hus, det er nok forfaldendt",
}

var visitResponse2 = models.VisitResponse{
	UserID:  user.ID,
	VisitID: visit1.ID,
	ActDate: time.Now(),
	ActTime: "10:00 AM",
	ActLat:  "37.7749",
	ActLong: "-122.4194",
	// Response data
	DebitorIsHome: true,

	AssetAtAddress: true,
	AssetDamaged:   false,

	CivilStatus:     models.Married,
	ChildrenUnder18: 10,
	ChildrenOver18:  10,
	ChildSupport:    4000,

	HasWork:  true,
	Position: "CEO",
	Salary:   50000,

	Creditor:   "nordania",
	DebtAmount: 1000000,
	Settlement: "forlig",

	PropertyType:      models.FreestandingHouse,
	MaintenanceStatus: models.WellMaintained,

	OwnershipStatus: "owner",

	Comments: "Meget flot hus, han er tydeligvis rig",
}
var visitResponse3 = models.VisitResponse{
	UserID:  user.ID,
	VisitID: visit1.ID,
	ActDate: time.Now(),
	ActTime: "10:00 AM",
	ActLat:  "37.7749",
	ActLong: "-122.4194",
	// Response data
	DebitorIsHome: true,

	AssetAtAddress: true,
	AssetDamaged:   false,

	CivilStatus:     models.Married,
	ChildrenUnder18: 0,
	ChildrenOver18:  0,
	ChildSupport:    0,

	HasWork:  true,
	Position: "janitor",
	Salary:   50000,

	Creditor:   "nordania",
	DebtAmount: 1000000,
	Settlement: "forlig",

	PropertyType:      models.Apartment,
	MaintenanceStatus: models.Deteriorated,

	OwnershipStatus: "owner",

	Comments: "Meget flot hus, han er tydeligvis rig",
}
var visitResponse4 = models.VisitResponse{
	UserID:  user.ID,
	VisitID: visit1.ID,
	ActDate: time.Now(),
	ActTime: "10:00 AM",
	ActLat:  "37.7749",
	ActLong: "-122.4194",
	// Response data
	DebitorIsHome: false,

	AssetAtAddress: false,
	AssetDamaged:   false,

	CivilStatus:     models.Single,
	ChildrenUnder18: 10,
	ChildrenOver18:  10,
	ChildSupport:    4000,

	HasWork:  true,
	Position: "CEO",
	Salary:   50000,

	Creditor:   "nordania",
	DebtAmount: 1000000,
	Settlement: "forlig",

	PropertyType:      models.SummerHouse,
	MaintenanceStatus: models.WellMaintained,

	OwnershipStatus: "renter",

	Comments: "Meget flot hus, han er tydeligvis rig",
}
