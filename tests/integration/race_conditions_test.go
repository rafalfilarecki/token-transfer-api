package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
	"token-transfer-api/internal/db"
	"token-transfer-api/pkg/graphql"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type RaceConditionSuite struct {
	suite.Suite
	server *httptest.Server
}

// SetupSuite initializes the test environment
func (s *RaceConditionSuite) SetupSuite() {
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
func (s *RaceConditionSuite) TearDownSuite() {
	s.server.Close()
	db.CloseDB()
}

// SetupTest resets the database state before each test
func (s *RaceConditionSuite) SetupTest() {
	s.resetDBState()
}

// resetDBState resets the database to a known state
func (s *RaceConditionSuite) resetDBState() {
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
func (s *RaceConditionSuite) TearDownTest() {
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
func (s *RaceConditionSuite) createWallet(address, balance string) {
	_, err := db.DB.Exec("INSERT INTO wallets (address, balance) VALUES ($1, $2) ON CONFLICT (address) DO UPDATE SET balance = $2",
		address, balance)
	assert.NoError(s.T(), err)
}

// getBalance gets a wallet's balance
func (s *RaceConditionSuite) getBalance(address string) string {
	var balance string
	err := db.DB.QueryRow("SELECT balance FROM wallets WHERE address = $1", address).Scan(&balance)
	assert.NoError(s.T(), err)
	return balance
}

// executeTransfer makes a GraphQL request to transfer tokens
func (s *RaceConditionSuite) executeTransfer(fromAddress, toAddress, amount string) (*graphQLResponse, error) {
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

// TestRaceConditions tests race conditions with concurrent transfers
func (s *RaceConditionSuite) TestRaceConditions() {
	// Set up a wallet with 10 tokens
	fromAddr := "0x0000000000000000000000000000000000000000"
	toAddr1 := "0x0000000000000000000000000000000000000001"
	toAddr2 := "0x0000000000000000000000000000000000000002"
	toAddr3 := "0x0000000000000000000000000000000000000003"

	s.createWallet(fromAddr, "10")

	var wg sync.WaitGroup
	wg.Add(3)

	results := make([]*graphQLResponse, 3)
	errors := make([]error, 3)

	// Credit transfer +1
	go func() {
		defer wg.Done()
		results[0], errors[0] = s.executeTransfer(toAddr1, fromAddr, "1")
	}()

	// Debit transfer -4
	go func() {
		defer wg.Done()
		results[1], errors[1] = s.executeTransfer(fromAddr, toAddr2, "4")
	}()

	// Debit transfer -7
	go func() {
		defer wg.Done()
		results[2], errors[2] = s.executeTransfer(fromAddr, toAddr3, "7")
	}()

	wg.Wait()

	// Verify all HTTP requests succeeded
	for i, err := range errors {
		assert.NoError(s.T(), err, "HTTP request %d failed", i)
	}

	// Check the final balance
	finalBalance := s.getBalance(fromAddr)

	// Count successful withdrawals
	successfulWithdrawals := 0
	if results[1] != nil && results[1].Errors == nil {
		successfulWithdrawals++
	}
	if results[2] != nil && results[2].Errors == nil {
		successfulWithdrawals++
	}

	// Validate possible outcomes based on final balance
	switch finalBalance {
	case "7":
		assert.Equal(s.T(), 1, successfulWithdrawals)
		assert.Nil(s.T(), results[1].Errors)    // -4 succeeded
		assert.NotNil(s.T(), results[2].Errors) // -7 failed

	case "4":
		assert.Equal(s.T(), 1, successfulWithdrawals)
		assert.NotNil(s.T(), results[1].Errors) // -4 failed
		assert.Nil(s.T(), results[2].Errors)    // -7 succeeded

	case "0":
		assert.Equal(s.T(), 2, successfulWithdrawals)
		assert.Nil(s.T(), results[1].Errors) // -4 succeeded
		assert.Nil(s.T(), results[2].Errors) // -7 succeeded

	default:
		s.T().Fatalf("Unexpected final balance: %s", finalBalance)
	}
}

// TestHighConcurrency tests many concurrent transfers
func (s *RaceConditionSuite) TestHighConcurrency() {
	// Create two wallets with 1000 tokens each
	wallet1 := "0xb000000000000000000000000000000000000001"
	wallet2 := "0xb000000000000000000000000000000000000002"

	s.createWallet(wallet1, "1000")
	s.createWallet(wallet2, "1000")

	const numTransfers = 10

	var wg sync.WaitGroup
	wg.Add(numTransfers * 2)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Launch concurrent transfers in both directions
	for i := 0; i < numTransfers; i++ {
		go func() {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			default:
				s.executeTransfer(wallet1, wallet2, "10")
			}
		}()

		go func() {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			default:
				s.executeTransfer(wallet2, wallet1, "5")
			}
		}()
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.T().Log("All transfers completed without deadlock")
	case <-ctx.Done():
		s.T().Fatal("Deadlock detected: transfers did not complete within timeout")
	}

	// Verify final balances
	balance1 := s.getBalance(wallet1)
	balance2 := s.getBalance(wallet2)

	// Calculate expected balances
	expected1 := 1000 - (10 * numTransfers) + (5 * numTransfers)
	expected2 := 1000 + (10 * numTransfers) - (5 * numTransfers)

	assert.Equal(s.T(), fmt.Sprint(expected1), balance1)
	assert.Equal(s.T(), fmt.Sprint(expected2), balance2)
}

// Run the race condition test suite
func TestRaceConditionSuite(t *testing.T) {
	suite.Run(t, new(RaceConditionSuite))
}
