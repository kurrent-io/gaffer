package dotenv

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// Load reads .env from the project root. If envName is provided,
// also loads .env.{envName} which overrides values from .env.
func Load(projectDir string, envName string) error {
	base := filepath.Join(projectDir, ".env")
	if _, err := os.Stat(base); err == nil {
		if err := godotenv.Load(base); err != nil {
			return err
		}
	}

	if envName != "" {
		override := filepath.Join(projectDir, ".env."+envName)
		if _, err := os.Stat(override); err == nil {
			if err := godotenv.Overload(override); err != nil {
				return err
			}
		}
	}

	return nil
}

// Credentials returns KurrentDB username and password from env vars.
func Credentials() (username, password string) {
	return os.Getenv("KURRENTDB_USERNAME"), os.Getenv("KURRENTDB_PASSWORD")
}
