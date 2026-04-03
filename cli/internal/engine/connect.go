package engine

import (
	"fmt"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/dotenv"
)

func Connect(connStr, projectRoot string) (*kurrentdb.Client, error) {
	if err := dotenv.Load(projectRoot, ""); err != nil {
		return nil, fmt.Errorf("loading .env: %w", err)
	}

	dbConfig, err := kurrentdb.ParseConnectionString(connStr)
	if err != nil {
		return nil, fmt.Errorf("invalid connection string: %w", err)
	}

	username, password := dotenv.Credentials()
	if username != "" {
		dbConfig.Username = username
		dbConfig.Password = password
	}
	dbConfig.Logger = kurrentdb.NoopLogging()

	client, err := kurrentdb.NewClient(dbConfig)
	if err != nil {
		return nil, fmt.Errorf("connecting to KurrentDB: %w", err)
	}
	return client, nil
}
