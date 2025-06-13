package initializers

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

func LoadEnvVariables() {
	if os.Getenv("PRODUCTION") != "True" { // or some similar guard
		if err := godotenv.Load(); err != nil {
			log.Println(".env file not found; relying on environment variables")
		}
	}
}
