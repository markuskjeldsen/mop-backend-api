package models

import (
	"time"

	"gorm.io/gorm"
)

type PropertyType string

const (
	PropertyFreestandingHouse PropertyType = "Fritlæggende hus"
	PropertyTownhouse         PropertyType = "Byhus"
	PropertyTerracedHouse     PropertyType = "Rækkehus"
	PropertySummerHouse       PropertyType = "Sommerhus"
	PropertyGardenColony      PropertyType = "Kolonihave"
	PropertyApartment         PropertyType = "Lejlighed"
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
	RightsNone      UserRights = "none"
)

type CivilStatus string

const (
	Married    CivilStatus = "Married"
	Single     CivilStatus = "Single"
	Cohabiting CivilStatus = "Cohabiting"
)

type Gender string

const (
	Male   Gender = "Male"
	Female Gender = "Female"
	Other  Gender = "Other"
)

type Risk string

const (
	LowRisk    Risk = "Low"
	MediumRisk Risk = "Medium"
	HighRisk   Risk = "High"
)

// models/models.go
type User struct {
	gorm.Model
	Name     string     `json:"name" binding:"required" gorm:"not null"`
	Username string     `json:"username" binding:"required" gorm:"not null"`
	Password string     `json:"password" binding:"required" gorm:"not null"`
	Rights   UserRights `json:"rights" gorm:"default:user"` // user, admin, developer
	Email    string     `json:"email"`
	Phone    string     `json:"phone"`
	Visits   []Visit    `json:"visits" gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
}

type Debitor struct {
	gorm.Model
	Name             string    `json:"name" gorm:"not null"`
	Phone            string    `json:"phone"`
	PhoneWork        string    `json:"phone_work"`
	Email            string    `json:"email"`
	Gender           Gender    `json:"gender" gorm:"not null"` // Male, Female, Other
	Birthday         time.Time `json:"birthday"`
	AdvoproDebitorId int       `json:"Advopro_debitor_id"`
	Risk             Risk      `json:"risk"` // Low, Medium, High
	SSN              string    `json:"ssn"`

	Notes  string  `json:"notes"`
	Visits []Visit `gorm:"many2many:visit_debitors;"`
}

// skal jeg lave en tabel som hedder sager
// også have debitorer knyttet til en sag?
// eller vil vi gerne have mulighed for at kunne besøge kun en debitor
// skal jeg så have forlobid med.
type VisitStatus struct {
	gorm.Model
	Text        string `json:"text"`
	Description string `json:"description"`
}

type VisitStatusLog struct {
	gorm.Model
	VisitID     uint      `json:"visit_id" gorm:"not null"`
	OldStatusID uint      `json:"old_status_id"`
	NewStatusID uint      `json:"new_status_id"`
	ChangedAt   time.Time `json:"changed_at" gorm:"autoCreateTime"`
	ChangedByID uint      `json:"changed_by_id"` // Optionally, reference User.ID
}

type Visit struct {
	gorm.Model
	UserID          uint             `json:"user_id"`
	Address         string           `json:"address"`
	Latitude        string           `json:"latitude"`
	Longitude       string           `json:"longitude"`
	Notes           string           `json:"notes"`
	Sagsnr          uint             `json:"sagsnr"`
	VisitDate       time.Time        `json:"visit_date"`
	VisitTime       string           `json:"visit_time"`
	VisitInterval   string           `json:"visit_interval"`
	Visited         bool             `json:"visited"`
	StatusID        uint             `json:"status_id" gorm:"not null;default:1"` // <-- Add this
	Status          VisitStatus      `json:"status" gorm:"foreignKey:StatusID"`   // <-- Keep this for relation
	Debitors        []Debitor        `json:"debitors" gorm:"many2many:visit_debitors;"`
	VisitResponse   *VisitResponse   `json:"visit_response" gorm:"foreignKey:VisitID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
	VisitStatusLogs []VisitStatusLog `json:"visit_status_logs" gorm:"foreignKey:VisitID"`
}

type VisitResponse struct {
	gorm.Model
	VisitID uint `json:"visit_id" binding:"required" gorm:"not null;unique"`

	// actual data
	ActDate     time.Time `json:"actual_date" binding:"required"`
	ActTime     string    `json:"actual_time" binding:"required"`
	ActLat      string    `json:"actual_latitude" binding:"required"`
	ActLong     string    `json:"actual_longitude" binding:"required"`
	PosAccuracy string    `json:"pos_accuracy" binding:"required"`

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
	OriginalName    string `json:"original_name"`
}
