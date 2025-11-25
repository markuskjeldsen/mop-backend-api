package api2

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/models"
)

/*
apiv2.GET("/visits", middleware.RequireAuthUser, api.GetVisits)
apiv2.GET("/visits/types", api.GetVisitTypes)
apiv2.GET("/visits/byId", middleware.RequireAuthUser, api.GetVisitsById)          //query parameter
apiv2.GET("/visits/byStatus", middleware.RequireAuthAdmin, api.GetVisitsByStatus) // query parameter
*/
func getVerifyUser(c *gin.Context) (models.User, bool) {
	u, ok := c.Get("user")
	if !ok {
		return models.User{}, false
	}
	user, ok := u.(models.User)
	if !ok {
		return models.User{}, false
	}
	return user, true
}

func GetVisits(c *gin.Context) {
	var users []models.User
	user, ok := getVerifyUser(c)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{})
	}

	switch user.Rights {
	case models.RightsUser:
		initializers.DB.Preload("Visits").Preload("Visits.Debitors").Find(&users, user.ID)
	case models.RightsAdmin:
		initializers.DB.Preload("Visits").Preload("Visits.Debitors").Where("id != 1").Find(&users)
	case models.RightsDeveloper:
		initializers.DB.Preload("Visits").Preload("Visits.Debitors").Find(&users)
	}
	c.JSON(http.StatusOK, users)
}
