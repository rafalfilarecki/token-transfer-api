package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSimpleTransfer(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req TransferRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
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

	reqBody := `{"query": "mutation { transfer(from_address: \"0x0000\", to_address: \"0x0001\", amount: \"100\") { balance } }"}`
	req := httptest.NewRequest("POST", "/query", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	var response TransferResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Errors != nil {
		t.Fatalf("Unexpected errors: %v", response.Errors)
	}

	data, ok := response.Data.(map[string]interface{})
	if !ok {
		t.Fatal("Data is not a map")
	}

	transfer, ok := data["transfer"].(map[string]interface{})
	if !ok {
		t.Fatal("Transfer is not a map")
	}

	balance, ok := transfer["balance"].(string)
	if !ok {
		t.Fatal("Balance is not a string")
	}

	if balance != "999900" {
		t.Errorf("Expected balance 999900, got %s", balance)
	}
}
