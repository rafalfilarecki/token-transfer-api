package db

import (
	"testing"

	"github.com/joho/godotenv"
)

func TestInitDB(t *testing.T) {
	if err := godotenv.Load("../.env"); err != nil {
		t.Fatalf("Failed to load .env file: %v", err)
	}

	err := InitDB()
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	if err := DB.Ping(); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}

	CloseDB()
}

func TestCloseDB(t *testing.T) {
	if err := godotenv.Load("../.env"); err != nil {
		t.Fatalf("Failed to load .env file: %v", err)
	}

	if err := InitDB(); err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	err := CloseDB()
	if err != nil {
		t.Fatalf("Failed to close database: %v", err)
	}

	if DB != nil {
		if err := DB.Ping(); err == nil {
			t.Fatal("Database should be closed but Ping succeeded")
		}
	}
}
