package main

import (
	"fmt"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/api"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/internal"
	"github.com/markuskjeldsen/mop-backend-api/middleware"
)

func init() {
	// Load environment variables and connect to the database
	initializers.LoadEnvVariables()
	initializers.ConnectToDB()
}

func main() {
	//test()
	start_server()
}

func test() {
	internal.CurrentDebtCase(114429)
	internal.GeneratePDFVisit(6)
}

func start_server() {
	fmt.Println("Starting server...")

	r := gin.New() // was gin.Default()
	r.Use(middleware.RequestLogger())
	r.Use(middleware.CORSMiddleware)
	r.Use(middleware.GeoIPBlocker("DK", "./static/GeoLite2-Country.mmdb"))
	// Trust your nginx proxy IP(s) or the private networks where your proxies live.
	// If nginx connects from 127.0.0.1 (same host):
	if err := r.SetTrustedProxies([]string{
		"127.0.0.1",  // loopback
		"10.0.0.0/8", // typical private ranges (optional)
		"172.16.0.0/12",
		"192.168.0.0/16",
		"::1",
	}); err != nil {
		panic(err)
	}

	apiv1 := r.Group("/api/v1") // Grouping routes under /api/v1
	{
		apiv1.GET("/health", api.Hello) // Adding a route to the group
		apiv1.GET("/verifytoken", middleware.RequireAuthUser, api.Verifytoken)

		apiv1.GET("/users", middleware.RequireAuthAdmin, api.GetUsers)          // Adding a route to the group
		apiv1.GET("/user", middleware.RequireAuthUser, api.GetUser)             // Adding a route to the group
		apiv1.GET("/users/:id", middleware.RequireAuthUser, api.GetUserByParam) // Adding a route to the group
		apiv1.PATCH("/users/:id", middleware.RequireAuthUser, api.Patch)
		apiv1.PATCH("/users/:id/password", middleware.RequireAuthUser, api.ChangePassword)

		apiv1.DELETE("/users/:id", middleware.RequireAuthAdmin, api.DeleteUser)

		apiv1.POST("/register", middleware.RequireAuthAdmin, api.CreateUser)
		apiv1.POST("/login", middleware.LoginAttemptLog, api.Login)
		apiv1.POST("/logout", middleware.RequireAuthUser, api.Logout)

		apiv1.GET("/visit-response/all", middleware.RequireAuthUser, api.Visit_responses)         // get all the responses
		apiv1.POST("/visit-response/create", middleware.RequireAuthUser, api.CreateVisitResponse) // make a response
		apiv1.POST("/visit-response/:id/images", middleware.RequireAuthUser, api.UploadVisitImage)

		apiv1.GET("/visits", middleware.RequireAuthUser, api.GetVisits)
		apiv1.GET("/visits/types", api.GetVisitTypes)
		apiv1.GET("/visits/byId", middleware.RequireAuthUser, api.GetVisitsById)          //query parameter
		apiv1.GET("/visits/byStatus", middleware.RequireAuthAdmin, api.GetVisitsByStatus) // query parameter
		apiv1.GET("/visits/debt", middleware.RequireAuthUser, api.DebtInformation)        // query parameter
		apiv1.DELETE("/visit/byId", middleware.RequireAuthAdmin, api.DeleteVisit)

		apiv1.GET("/visits/AvailableVisit", middleware.RequireAuthAdmin, api.AvailableVisitCreation) // gets visits that can be created
		apiv1.POST("/visits/create", middleware.RequireAuthAdmin, api.VisitCreation)                 // creates thoses visits
		apiv1.GET("/visits/create", middleware.RequireAuthAdmin, api.CreatedVisits)                  // retrives the created visits that have not yet been planned

		apiv1.POST("/visits/visitfile", middleware.RequireAuthAdmin, api.VisitFile)     // generates a visit excel file so the visits can be planned without making another visit
		apiv1.POST("/visits/plan", middleware.RequireAuthAdmin, api.PlanVisit)          // here visits are planned
		apiv1.GET("/visits/planned", middleware.RequireAuthAdmin, api.PlannedVisits)    // here are the planned visits
		apiv1.PATCH("/visits/planned/:id", middleware.RequireAuthAdmin, api.PatchVisit) // here are the planned visits

		//send the letters
		apiv1.POST("/visit/letterSent", middleware.RequireAuthAdmin, api.VisitLetterSent) // remember GetQuery("id")

		apiv1.GET("/visit/pdf", middleware.RequireAuthAdmin, api.VisitPDF)
		apiv1.POST("visit/reviewed", middleware.RequireAuthAdmin, api.ReviewedVisit)
	}

	r.StaticFile("/", "./static/index.html")
	r.StaticFile("/favicon-dark.ico", "./static/favicon-dark.ico")
	r.StaticFile("/favicon-light.ico", "./static/favicon-light.ico")
	r.Static("/assets", "./static/assets/")
	r.NoRoute(func(c *gin.Context) {
		c.File("./static/index.html")
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	if err := r.Run(":" + port); err != nil {
		fmt.Printf("Server error: %v\n", err)
	} else {
		fmt.Printf("Server has closed\n")
	}
}
