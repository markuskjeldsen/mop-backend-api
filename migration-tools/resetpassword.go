package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/markuskjeldsen/mop-backend-api/initializers"
	"github.com/markuskjeldsen/mop-backend-api/models"
	"golang.org/x/crypto/bcrypt"
)

func init() {
	initializers.LoadEnvVariables()
	initializers.ConnectToDB()
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: app <number>")
		return
	}
	n, err := strconv.Atoi(os.Args[1])
	if err != nil {
		panic(err)
	}
	fmt.Println("id:", n)
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("pass"), 14)
	var user models.User
	user.ID = uint(n)
	initializers.DB.Model(&user).Update("password", string(hashedPassword))
}
