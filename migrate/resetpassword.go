package main

import (
	"fmt"

	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/models"
	"golang.org/x/crypto/bcrypt"
)

func ResetPassword(id uint) {
	fmt.Println("id:", id)
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("pass"), 14)
	var user models.User
	user.ID = uint(id)
	initializers.DB.Model(&user).Update("password", string(hashedPassword))
}
