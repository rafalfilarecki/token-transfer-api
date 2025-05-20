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

type EdgeCaseSuite struct {
	suite.Suite
	server *httptest.Server
}

// SetupSuite initializes the test environment
func (s *EdgeCaseSuite) SetupSuite() {
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
func (s *EdgeCaseSuite) TearDownSuite() {
	s.server.Close()
	db.CloseDB()
}

// SetupTest resets the database state before each test
func (s *EdgeCaseSuite) SetupTest() {
	s.resetDBState()
}

// resetDBState resets the database to a known state
func (s *EdgeCaseSuite) resetDBState() {
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
	s.createWallet("0x0000000000000000000000000000000000000003", "0")
}

// TearDownTest restores the database after each test
func (s *EdgeCaseSuite) TearDownTest() {
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
func (s *EdgeCaseSuite) createWallet(address, balance string) {
	_, err := db.DB.Exec("INSERT INTO wallets (address, balance) VALUES ($1, $2) ON CONFLICT (address) DO UPDATE SET balance = $2",
		address, balance)
	assert.NoError(s.T(), err)
}

// getBalance gets a wallet's balance
func (s *EdgeCaseSuite) getBalance(address string) string {
	var balance string
	err := db.DB.QueryRow("SELECT balance FROM wallets WHERE address = $1", address).Scan(&balance)
	assert.NoError(s.T(), err)
	return balance
}

// executeTransfer makes a GraphQL request to transfer tokens
func (s *EdgeCaseSuite) executeTransfer(fromAddress, toAddress, amount string) (*graphQLResponse, error) {
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

// TestInsufficientBalance tests transfer with insufficient balance
func (s *EdgeCaseSuite) TestInsufficientBalance() {
	fromAddr := "0x0000000000000000000000000000000000000000"
	toAddr := "0x0000000000000000000000000000000000000001"

	result, err := s.executeTransfer(fromAddr, toAddr, "2000000")
	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), result.Errors)

	// Verify error message
	assert.Contains(s.T(), result.Errors[0]["message"], "insufficient balance")

	// Verify balances unchanged
	assert.Equal(s.T(), "1000000", s.getBalance(fromAddr))
	assert.Equal(s.T(), "0", s.getBalance(toAddr))
}

// TestInvalidAmount tests transfer with invalid amount
func (s *EdgeCaseSuite) TestInvalidAmount() {
	fromAddr := "0x0000000000000000000000000000000000000000"
	toAddr := "0x0000000000000000000000000000000000000001"

	// Zero amount
	result, err := s.executeTransfer(fromAddr, toAddr, "0")
	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), result.Errors)
	assert.Contains(s.T(), result.Errors[0]["message"], "invalid amount")

	// Negative amount
	result, err = s.executeTransfer(fromAddr, toAddr, "-100")
	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), result.Errors)
	assert.Contains(s.T(), result.Errors[0]["message"], "invalid amount")

	// Verify balances unchanged
	assert.Equal(s.T(), "1000000", s.getBalance(fromAddr))
	assert.Equal(s.T(), "0", s.getBalance(toAddr))
}

// TestNonExistentSender tests transfer from non-existent wallet
func (s *EdgeCaseSuite) TestNonExistentSender() {
	fromAddr := "0xnonexistent"
	toAddr := "0x0000000000000000000000000000000000000001"

	result, err := s.executeTransfer(fromAddr, toAddr, "100")
	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), result.Errors)
	assert.Contains(s.T(), result.Errors[0]["message"], "sender wallet does not exist")
}

// TestTransferToNewWallet tests transfer to a non-existent wallet
func (s *EdgeCaseSuite) TestTransferToNewWallet() {
	fromAddr := "0x0000000000000000000000000000000000000000"
	toAddr := "0x9999999999999999999999999999999999999999"

	result, err := s.executeTransfer(fromAddr, toAddr, "250")
	assert.NoError(s.T(), err)
	assert.Nil(s.T(), result.Errors)

	// Verify response
	transferData, ok := result.Data["transfer"].(map[string]interface{})
	assert.True(s.T(), ok)
	assert.Equal(s.T(), "999750", transferData["balance"])

	// Verify database state - new wallet should be created
	assert.Equal(s.T(), "999750", s.getBalance(fromAddr))
	assert.Equal(s.T(), "250", s.getBalance(toAddr))
}

// TestLargeAmount tests transfer with large amount
func (s *EdgeCaseSuite) TestLargeAmount() {
	fromAddr := "0x0000000000000000000000000000000000000000"
	toAddr := "0x0000000000000000000000000000000000000001"

	// Very large amount that fits in the DECIMAL(78, 0) type
	result, err := s.executeTransfer(fromAddr, toAddr, "123456789012345678901234567890")
	assert.NoError(s.T(), err)

	// Should fail due to insufficient balance
	assert.NotNil(s.T(), result.Errors)
	assert.Contains(s.T(), result.Errors[0]["message"], "insufficient balance")

	// Set large balance and try again
	s.createWallet(fromAddr, "9999999999999999999999999999999999999999999999")

	result, err = s.executeTransfer(fromAddr, toAddr, "1234567890123456789012345678901234567890")
	assert.NoError(s.T(), err)
	assert.Nil(s.T(), result.Errors)

	// Verify balances
	expectedSenderBalance := "9999999999999999999999998765432109876543210999999999"
	assert.Equal(s.T(), expectedSenderBalance, s.getBalance(fromAddr))
	assert.Equal(s.T(), "1234567890123456789012345678901234567890", s.getBalance(toAddr))
}

// TestMalformedRequests tests handling of invalid GraphQL requests
func (s *EdgeCaseSuite) TestMalformedRequests() {
	// Missing required field
	mutation := `mutation {
		transfer(
			from_address: "0x0000000000000000000000000000000000000000", 
			to_address: "0x0000000000000000000000000000000000000001"
			# amount missing
		) {
			balance
		}
	}`

	reqBody, _ := json.Marshal(graphQLRequest{Query: mutation})
	resp, err := http.Post(s.server.URL, "application/json", bytes.NewBuffer(reqBody))
	assert.NoError(s.T(), err)

	var result graphQLResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), result.Errors)

	// Invalid GraphQL syntax
	invalidQuery := `mutation {
		transfer(
			from_address: "0x0000000000000000000000000000000000000000", 
			to_address: "0x0000000000000000000000000000000000000001", 
			amount: "100"
		) {
			balance
		` // Missing closing brace

	reqBody, _ = json.Marshal(graphQLRequest{Query: invalidQuery})
	resp, err = http.Post(s.server.URL, "application/json", bytes.NewBuffer(reqBody))
	assert.NoError(s.T(), err)

	err = json.NewDecoder(resp.Body).Decode(&result)
	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), result.Errors)
}

// Run the edge case test suite
func TestEdgeCaseSuite(t *testing.T) {
	suite.Run(t, new(EdgeCaseSuite))
}
