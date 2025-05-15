package main

import (
	"encoding/json"
	"log"
	"net/http"
	"token-transfer-api/db"

	"github.com/joho/godotenv"
)

type TransferRequest struct {
	Query string `json:"query"`
}

type TransferResponse struct {
	Data   interface{} `json:"data,omitempty"`
	Errors []Error     `json:"errors,omitempty"`
}

type Error struct {
	Message string `json:"message"`
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	if err := db.InitDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.CloseDB()

	http.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		var req TransferRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(TransferResponse{
				Errors: []Error{{Message: "Invalid request"}},
			})
			return
		}

		response := TransferResponse{
			Data: map[string]interface{}{
				"transfer": map[string]interface{}{
					"balance": "999900",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
