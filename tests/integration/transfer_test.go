package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"token-transfer-api/internal/db"
	"token-transfer-api/pkg/graphql"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
)

type graphQLResponse struct {
	Data   map[string]interface{} `json:"data,omitempty"`
	Errors []interface{}          `json:"errors,omitempty"`
}

func TestTransferIntegration(t *testing.T) {
	// Setup
	if err := godotenv.Load("../../.env"); err != nil {
		t.Logf("No .env file found")
	}

	if err := db.InitDB(); err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.CloseDB()

	// Setup isolated test database state
	dbSetup(t)
	defer dbReset(t)

	// Setup GraphQL handler
	handler := graphql.NewHandler()
	server := httptest.NewServer(handler)
	defer server.Close()

	// Execute query
	mutation := `mutation {
		transfer(
			from_address: "0x0000000000000000000000000000000000000000", 
			to_address: "0x0000000000000000000000000000000000000001", 
			amount: "500"
		) {
			balance
		}
	}`

	reqBody, _ := json.Marshal(map[string]string{"query": mutation})
	resp, err := http.Post(server.URL, "application/json", bytes.NewBuffer(reqBody))
	assert.NoError(t, err)
	defer resp.Body.Close()

	// Parse response
	var result graphQLResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	assert.NoError(t, err)
	assert.Nil(t, result.Errors)

	// Verify transfer data
	if result.Data != nil {
		transferData, ok := result.Data["transfer"].(map[string]interface{})
		if assert.True(t, ok, "Transfer data should be a map") {
			assert.Equal(t, "999500", transferData["balance"])
		}
	}

	// Verify database state
	var senderBalance, receiverBalance string
	err = db.DB.QueryRow("SELECT balance FROM wallets WHERE address = $1", "0x0000000000000000000000000000000000000000").Scan(&senderBalance)
	assert.NoError(t, err)
	assert.Equal(t, "999500", senderBalance)

	err = db.DB.QueryRow("SELECT balance FROM wallets WHERE address = $1", "0x0000000000000000000000000000000000000001").Scan(&receiverBalance)
	assert.NoError(t, err)
	assert.Equal(t, "500", receiverBalance)
}

// dbSetup creates a known database state for testing
func dbSetup(t *testing.T) {
	// Create a separate test schema
	_, err := db.DB.Exec("CREATE SCHEMA IF NOT EXISTS test")
	assert.NoError(t, err)

	// Save original state
	_, err = db.DB.Exec(`CREATE TEMPORARY TABLE temp_wallets AS SELECT * FROM wallets`)
	assert.NoError(t, err)

	// Reset wallets to known state
	_, err = db.DB.Exec(`UPDATE wallets SET balance = CASE address 
		WHEN '0x0000000000000000000000000000000000000000' THEN 1000000 
		ELSE 0 END`)
	assert.NoError(t, err)

	// Clear transfers
	_, err = db.DB.Exec("TRUNCATE TABLE transfers")
	assert.NoError(t, err)
}

// dbReset restores the database to its original state
func dbReset(t *testing.T) {
	// Restore original wallet state
	_, err := db.DB.Exec(`UPDATE wallets w SET 
		balance = t.balance
		FROM temp_wallets t 
		WHERE w.address = t.address`)
	assert.NoError(t, err)

	// Clean up
	_, err = db.DB.Exec("DROP TABLE IF EXISTS temp_wallets")
	assert.NoError(t, err)
}
