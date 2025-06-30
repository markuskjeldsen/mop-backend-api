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

	initializers.DB.Exec("PRAGMA foreign_keys = OFF;")
	initializers.DB.Exec("DROP TABLE IF EXISTS users;")

	// the many2many connection with debitor and visits need a connection table
	initializers.DB.Exec("DROP TABLE IF EXISTS visit_debitors;")
	initializers.DB.Exec("DROP TABLE IF EXISTS debitors;")
	initializers.DB.Exec("DROP TABLE IF EXISTS visits;")

	initializers.DB.Exec("DROP TABLE IF EXISTS visit_responses;")
	initializers.DB.Exec("PRAGMA foreign_keys = ON;")

	initializers.DB.AutoMigrate(&models.User{})          // users
	initializers.DB.AutoMigrate(&models.Debitor{})       // debitors
	initializers.DB.AutoMigrate(&models.Visit{})         // visits (references debitors with many2many)
	initializers.DB.AutoMigrate(&models.VisitResponse{}) // visit_responses
	initializers.DB.AutoMigrate(&models.Visit{}, &models.VisitResponse{}, &models.VisitStatus{})

	initializers.DB.Create(&status1)
	initializers.DB.Create(&status2)
	initializers.DB.Create(&status3)
	initializers.DB.Create(&status4)
	initializers.DB.Create(&status5)

	//Hash the password
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(user.Password), 14)
	hashedPassword1, _ := bcrypt.GenerateFromPassword([]byte(user1.Password), 14)

	user.Password = string(hashedPassword)
	user1.Password = string(hashedPassword1)
	//create some users
	initializers.DB.Create(&root)
	initializers.DB.Create(&user)  // Save the user to the database
	initializers.DB.Create(&user1) // Save the user to the database

	//create debitors
	initializers.DB.Create(&db1)
	initializers.DB.Create(&db2)
	initializers.DB.Create(&db3)

	// create some visits to the debitors
	visit1.UserID = user.ID
	visit1.Debitors = []models.Debitor{db1, db3}

	visit2.UserID = user.ID
	visit2.Debitors = []models.Debitor{db2, db3}

	initializers.DB.Create(&visit1) // Save the visit to the database
	initializers.DB.Create(&visit2) // Save the visit to the database

	//create some responses to the visits
	visitResponse1.VisitID = visit1.ID
	visitResponse2.VisitID = visit2.ID
	initializers.DB.Create(&visitResponse1) // Save the visit response to the database
	initializers.DB.Create(&visitResponse2) // Save the visit response to the database

	//create some visits to the debitors
	visit3.UserID = user1.ID
	visit4.UserID = user1.ID
	visit5.UserID = user1.ID
	visit6.UserID = user1.ID

	// add debitors to the visits
	visit3.Debitors = []models.Debitor{db1}
	visit4.Debitors = []models.Debitor{db2}
	visit5.Debitors = []models.Debitor{db3}
	visit6.Debitors = []models.Debitor{db1, db2}

	initializers.DB.Create(&visit3) // Save the visit to the database
	initializers.DB.Create(&visit4) // Save the visit to the database
	initializers.DB.Create(&visit5) // Save the visit to the database
	initializers.DB.Create(&visit6)

	visitResponse3.VisitID = visit3.ID
	visitResponse4.VisitID = visit4.ID
	initializers.DB.Create(&visitResponse3) // Save the visit response to the database
	initializers.DB.Create(&visitResponse4) // Save the visit response to the database

}

// placeholder information
var status1 = models.VisitStatus{
	Text: "Not planned",
}
var status2 = models.VisitStatus{
	Text: "planned",
}
var status3 = models.VisitStatus{
	Text: "ready",
}
var status4 = models.VisitStatus{
	Text: "to review",
}
var status5 = models.VisitStatus{
	Text: "exported",
}

var root = models.User{
	Username: "root",
	Password: "d",
	Email:    "",
	Phone:    "",
}
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
var db1 = models.Debitor{
	Name:             "Cindy Lou",
	Phone:            "1234567890",
	PhoneWork:        "0987654321",
	Email:            "cindy@example.com",
	Gender:           models.Female,
	Birthday:         time.Date(1995, 5, 10, 0, 0, 0, 0, time.UTC),
	AdvoproDebitorId: 45,
	Risk:             models.Low,
	SSN:              "140599-0013",
	Notes:            "Friendly and prompt payer.",
}
var db2 = models.Debitor{
	Name:             "Ebenezer Scrooge",
	Phone:            "2223334444",
	PhoneWork:        "3334445555",
	Email:            "scrooge@example.com",
	Gender:           models.Male,
	Birthday:         time.Date(1970, 12, 25, 0, 0, 0, 0, time.UTC),
	AdvoproDebitorId: 13,
	Risk:             models.High,
	SSN:              "020202-3213",
	Notes:            "High risk, late payer.",
}
var db3 = models.Debitor{
	Name:             "Grinch",
	Phone:            "5556667777",
	PhoneWork:        "8889990000",
	Email:            "grinch@example.com",
	Gender:           models.Other,
	Birthday:         time.Date(1982, 6, 1, 0, 0, 0, 0, time.UTC),
	AdvoproDebitorId: 99,
	Risk:             models.Medium,
	SSN:              "140205-0013",
	Notes:            "Sometimes cooperates.",
}
var visit1 = models.Visit{
	UserID:        user.ID,
	Address:       "123 Main St",
	VisitInterval: "10:00-13:00",
	Latitude:      "37.7749",
	Longitude:     "-122.4194",
	Notes:         "First visit",
	Sagsnr:        1,
	VistDate:      time.Now(),
	VisitTime:     "10:00 AM",
	Debitors:      []models.Debitor{db1, db2},
}
var visit2 = models.Visit{
	UserID:        user.ID,
	Address:       "123 Main St",
	VisitInterval: "10:00-13:00",
	Latitude:      "37.7749",
	Longitude:     "-122.4194",
	Notes:         "First visit",
	Sagsnr:        2,
	VistDate:      time.Now(),
	VisitTime:     "12:00 AM",
	Debitors:      []models.Debitor{db2, db3},
}
var visit3 = models.Visit{
	UserID:        user.ID,
	Address:       "1337 Main St",
	Debitors:      []models.Debitor{db3},
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
	Debitors:      []models.Debitor{db2},
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
	Debitors:      []models.Debitor{db1},
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
	Debitors:      []models.Debitor{db1},
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
