package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"token-transfer-api/internal/db"
	"token-transfer-api/pkg/graphql"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type BasicTransferSuite struct {
	suite.Suite
	server *httptest.Server
}

type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data   map[string]interface{}   `json:"data,omitempty"`
	Errors []map[string]interface{} `json:"errors,omitempty"`
}

// SetupSuite initializes the test environment
func (s *BasicTransferSuite) SetupSuite() {
	if err := godotenv.Load("../../.env"); err != nil {
		s.T().Logf("No .env file found")
	}

	if err := db.InitDB(); err != nil {
		s.T().Fatalf("Failed to initialize database: %v", err)
	}

	// Setup GraphQL handler
	handler := graphql.NewHandler()
	s.server = httptest.NewServer(handler)
}

// TearDownSuite cleans up the test environment
func (s *BasicTransferSuite) TearDownSuite() {
	s.server.Close()
	db.CloseDB()
}

// SetupTest resets the database state before each test
func (s *BasicTransferSuite) SetupTest() {
	s.resetDBState()
}

// resetDBState resets the database to a known state
func (s *BasicTransferSuite) resetDBState() {
	// Save original state
	_, err := db.DB.Exec(`CREATE TEMPORARY TABLE IF NOT EXISTS temp_wallets AS SELECT * FROM wallets`)
	assert.NoError(s.T(), err)

	// Reset wallets to known state
	_, err = db.DB.Exec(`TRUNCATE TABLE transfers`)
	assert.NoError(s.T(), err)

	// Set initial balances
	_, err = db.DB.Exec(`UPDATE wallets SET balance = CASE address 
		WHEN '0x0000000000000000000000000000000000000000' THEN 1000000 
		ELSE 0 END`)
	assert.NoError(s.T(), err)

	// Ensure test wallets exist
	s.createWallet("0x0000000000000000000000000000000000000001", "0")
	s.createWallet("0x0000000000000000000000000000000000000002", "0")
}

// TearDownTest restores the database after each test
func (s *BasicTransferSuite) TearDownTest() {
	// Restore original wallet state
	_, err := db.DB.Exec(`UPDATE wallets w SET 
		balance = t.balance
		FROM temp_wallets t 
		WHERE w.address = t.address`)
	assert.NoError(s.T(), err)

	// Clean up
	_, err = db.DB.Exec("DROP TABLE IF EXISTS temp_wallets")
	assert.NoError(s.T(), err)
}

// createWallet creates a wallet with the specified balance
func (s *BasicTransferSuite) createWallet(address, balance string) {
	_, err := db.DB.Exec("INSERT INTO wallets (address, balance) VALUES ($1, $2) ON CONFLICT (address) DO UPDATE SET balance = $2",
		address, balance)
	assert.NoError(s.T(), err)
}

// getBalance gets a wallet's balance
func (s *BasicTransferSuite) getBalance(address string) string {
	var balance string
	err := db.DB.QueryRow("SELECT balance FROM wallets WHERE address = $1", address).Scan(&balance)
	assert.NoError(s.T(), err)
	return balance
}

// executeTransfer makes a GraphQL request to transfer tokens
func (s *BasicTransferSuite) executeTransfer(fromAddress, toAddress, amount string) (*graphQLResponse, error) {
	mutation := fmt.Sprintf(`mutation {
		transfer(
			from_address: "%s", 
			to_address: "%s", 
			amount: "%s"
		) {
			balance
		}
	}`, fromAddress, toAddress, amount)

	reqBody, _ := json.Marshal(graphQLRequest{Query: mutation})
	resp, err := http.Post(s.server.URL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result graphQLResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	return &result, err
}

// TestSuccessfulTransfer tests a valid token transfer
func (s *BasicTransferSuite) TestSuccessfulTransfer() {
	fromAddr := "0x0000000000000000000000000000000000000000"
	toAddr := "0x0000000000000000000000000000000000000001"

	result, err := s.executeTransfer(fromAddr, toAddr, "500")
	assert.NoError(s.T(), err)
	assert.Nil(s.T(), result.Errors)

	// Verify response
	transferData, ok := result.Data["transfer"].(map[string]interface{})
	assert.True(s.T(), ok)
	assert.Equal(s.T(), "999500", transferData["balance"])

	// Verify database state
	assert.Equal(s.T(), "999500", s.getBalance(fromAddr))
	assert.Equal(s.T(), "500", s.getBalance(toAddr))
}

// TestMultipleTransfers tests a series of transfers
func (s *BasicTransferSuite) TestMultipleTransfers() {
	fromAddr := "0x0000000000000000000000000000000000000000"
	toAddr1 := "0x0000000000000000000000000000000000000001"
	toAddr2 := "0x0000000000000000000000000000000000000002"

	// First transfer
	result, err := s.executeTransfer(fromAddr, toAddr1, "300")
	assert.NoError(s.T(), err)
	assert.Nil(s.T(), result.Errors)

	// Second transfer
	result, err = s.executeTransfer(fromAddr, toAddr2, "400")
	assert.NoError(s.T(), err)
	assert.Nil(s.T(), result.Errors)

	// Transfer between recipient wallets
	result, err = s.executeTransfer(toAddr1, toAddr2, "100")
	assert.NoError(s.T(), err)
	assert.Nil(s.T(), result.Errors)

	// Verify final balances
	assert.Equal(s.T(), "999300", s.getBalance(fromAddr))
	assert.Equal(s.T(), "200", s.getBalance(toAddr1))
	assert.Equal(s.T(), "500", s.getBalance(toAddr2))
}

// TestTransferHistory verifies that transfer history is properly recorded
func (s *BasicTransferSuite) TestTransferHistory() {
	fromAddr := "0x0000000000000000000000000000000000000000"
	toAddr := "0x0000000000000000000000000000000000000001"

	// Execute multiple transfers
	s.executeTransfer(fromAddr, toAddr, "100")
	s.executeTransfer(fromAddr, toAddr, "200")
	s.executeTransfer(toAddr, fromAddr, "50")

	// Verify transfer records
	rows, err := db.DB.Query("SELECT from_address, to_address, amount FROM transfers ORDER BY id")
	assert.NoError(s.T(), err)
	defer rows.Close()

	var transfers []struct {
		FromAddress string
		ToAddress   string
		Amount      string
	}

	for rows.Next() {
		var transfer struct {
			FromAddress string
			ToAddress   string
			Amount      string
		}
		err := rows.Scan(&transfer.FromAddress, &transfer.ToAddress, &transfer.Amount)
		assert.NoError(s.T(), err)
		transfers = append(transfers, transfer)
	}

	assert.Equal(s.T(), 3, len(transfers))

	assert.Equal(s.T(), fromAddr, transfers[0].FromAddress)
	assert.Equal(s.T(), toAddr, transfers[0].ToAddress)
	assert.Equal(s.T(), "100", transfers[0].Amount)

	assert.Equal(s.T(), fromAddr, transfers[1].FromAddress)
	assert.Equal(s.T(), toAddr, transfers[1].ToAddress)
	assert.Equal(s.T(), "200", transfers[1].Amount)

	assert.Equal(s.T(), toAddr, transfers[2].FromAddress)
	assert.Equal(s.T(), fromAddr, transfers[2].ToAddress)
	assert.Equal(s.T(), "50", transfers[2].Amount)
}

// Run the integration test suite
func TestBasicTransferSuite(t *testing.T) {
	suite.Run(t, new(BasicTransferSuite))
}
