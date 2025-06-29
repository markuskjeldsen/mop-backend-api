package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/api"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
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
	server := "MOPSRV01\\SQL1"
	database := "AdvoPro"
	results, err := api.ExecuteQuery(server, database, 636004)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%v\n", results)
}

func start_server() {
	fmt.Println("Starting server...")

	r := gin.New() // was gin.Default()
	r.Use(middleware.RequestLogger())
	r.Use(middleware.CORSMiddleware)
	r.SetTrustedProxies(nil)

	r.GET("/", func(c *gin.Context) { c.Status(200) }) // Health check route
	r.GET("/routes", func(c *gin.Context) {            // to check
		endpoints := []string{}
		for _, route := range r.Routes() {
			endpoints = append(endpoints, route.Method+":"+route.Path)
		}
		c.JSON(200, endpoints)
	})

	apiv1 := r.Group("/api/v1") // Grouping routes under /api/v1
	{
		apiv1.GET("/", api.Hello)                                      // Adding a route to the group
		apiv1.GET("/users", middleware.RequireAuthAdmin, api.GetUsers) // Adding a route to the group
		apiv1.GET("/user", middleware.RequireAuthUser, api.GetUser)    // Adding a route to the group
		apiv1.PATCH("/user", middleware.RequireAuthUser, api.Patch)

		apiv1.POST("/register", middleware.RequireAuthAdmin, api.CreateUser)
		apiv1.POST("/login", api.Login)
		apiv1.POST("/logout", middleware.RequireAuthUser, api.Logout)

		apiv1.GET("/visit_responses", middleware.RequireAuthUser, api.Visit_responses)  // get all the responses
		apiv1.POST("/visit_responses", middleware.RequireAuthUser, api.Create_response) // make a response
		apiv1.GET("/visits", middleware.RequireAuthUser, api.GetVisits)
		apiv1.GET("/verifytoken", middleware.RequireAuthUser, api.Verifytoken)

	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	fmt.Println("ALLOW_ORIGIN:", os.Getenv("ALLOW_ORIGIN"))
	fmt.Printf("Server is running on port %s\n", port)
	if err := r.Run(":" + port); err != nil {
		fmt.Printf("Server error: %v\n", err)
	} else {
		fmt.Printf("Server has closed\n")
	}
}
