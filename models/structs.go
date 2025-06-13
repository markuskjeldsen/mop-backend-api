package models

import (
	"time"

	"gorm.io/gorm"
)

type PropertyType string

const (
	FreestandingHouse PropertyType = "Fritlæggende hus"
	Townhouse         PropertyType = "Byhus"
	TerracedHouse     PropertyType = "Rækkehus"
	SummerHouse       PropertyType = "Sommerhus"
	GardenColony      PropertyType = "Kolonihave"
	Apartment         PropertyType = "Lejlighed"
)

type MaintenanceStatus string

const (
	WellMaintained MaintenanceStatus = "Velholdt"
	Deteriorated   MaintenanceStatus = "Forfalden"
)

type UserRights string

const (
	RightsAdmin     UserRights = "admin"
	RightsDeveloper UserRights = "developer"
	RightsUser      UserRights = "user"
)

type CivilStatus string

const (
	Married    CivilStatus = "Married"
	Single     CivilStatus = "Single"
	Cohabiting CivilStatus = "Cohabiting"
)

// models/models.go
type User struct {
	gorm.Model
	Username       string          `json:"username" binding:"required" gorm:"not null"`
	Password       string          `json:"password" binding:"required" gorm:"not null"`
	Rights         UserRights      `json:"rights" gorm:"default:user"` // user, admin, developer
	Email          string          `json:"email"`
	Phone          string          `json:"phone"`
	Visits         []Visit         `json:"visits" gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
	VisitResponses []VisitResponse `json:"visit_responses" gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
}

type Visit struct {
	gorm.Model
	UserID        uint      `json:"user_id"`
	Address       string    `json:"address"`
	DebitorName   string    `json:"debitor_name"`
	DebitorPhone  string    `json:"debitor_phone"`
	Latitude      string    `json:"latitude"`
	Longitude     string    `json:"longitude"`
	Notes         string    `json:"notes"`
	Sagsnr        uint      `json:"sagsnr"`
	VistDate      time.Time `json:"visit_date"`
	VisitTime     string    `json:"visit_time"`
	VisitInterval string    `json:"visit_interval"`
	Visited       bool      `json:"visited"`
}

type VisitResponse struct {
	gorm.Model
	UserID  uint `json:"user_id" gorm:"not null"`
	VisitID uint `json:"visit_id" binding:"required" gorm:"not null"`

	// actual data
	ActDate time.Time `json:"actual_date"`
	ActTime string    `json:"actual_time"`
	ActLat  string    `json:"actual_latitude"`
	ActLong string    `json:"actual_longitude"`

	// response data
	DebitorIsHome   bool `json:"debitor_is_home"`
	PaymentReceived bool `json:"payment_received"`
	AssetAtAddress  bool `json:"asset_at_address"`
	AssetDelivered  bool `json:"asset_delivered"`
	AssetDamaged    bool `json:"asset_damaged"` // if then discribe
	KeysGiven       bool `json:"keys_given"`
	KeysReceived    bool `json:"keys_received"`

	SFSigned bool `json:"sf_signed"`
	SESigned bool `json:"se_signed"`

	CivilStatus CivilStatus `json:"civil_status"`

	//children
	ChildrenUnder18 uint    `json:"children_under_18"`
	ChildrenOver18  uint    `json:"children_over_18"`
	ChildSupport    float32 `json:"child_support"`

	//work
	HasWork  bool    `json:"has_work"`
	Position string  `json:"position"`
	Salary   float32 `json:"salary"`

	PensionPayment float32 `json:"pension_payment"`

	IncomePayment float32 `json:"income_payment"`

	MonthlyDisposableAmount float32 `json:"monthly_disposable_amount"`

	// debt
	Creditor    string  `json:"creditor"`
	DebtAmount  float32 `json:"debt_amount"`
	Settlement  string  `json:"settlement"`
	Creditor2   string  `json:"creditor_2"`
	DebtAmount2 float32 `json:"debt_amount_2"`
	Settlement2 string  `json:"settlement_2"`
	Creditor3   string  `json:"creditor_3"`
	DebtAmount3 float32 `json:"debt_amount_3"`
	Settlement3 string  `json:"settlement_3"`

	// property
	PropertyType      PropertyType      `json:"property_type"`
	MaintenanceStatus MaintenanceStatus `json:"maintenance_status"`

	//Ownership
	OwnershipStatus string `json:"ownership_status"` // owner, tenant, other

	Comments string `json:"comments"` // free text field for comments
	// images
	Images []VisitResponseImage `json:"images" gorm:"foreignKey:VisitResponseID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
}

type VisitResponseImage struct {
	gorm.Model
	VisitResponseID uint   `json:"visit_response_id"`
	ImagePath       string `json:"image_path"`
}
