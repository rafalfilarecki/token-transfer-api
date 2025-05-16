package main

import (
	"log"
	"net/http"
	"token-transfer-api/internal/db"
	"token-transfer-api/pkg/graphql"

	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	// Initialize database
	if err := db.InitDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.CloseDB()

	// Setup GraphQL handler
	handler := graphql.NewHandler()

	// Start server
	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}
