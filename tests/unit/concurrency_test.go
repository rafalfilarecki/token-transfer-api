package unit

import (
	"math/big"
	"sync"
	"testing"
	"time"
	"token-transfer-api/internal/db"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// ConcurrencyTestSuite tests race conditions and concurrent transfers
type ConcurrencyTestSuite struct {
	suite.Suite
}

func (s *ConcurrencyTestSuite) SetupSuite() {
	if err := godotenv.Load("../../.env"); err != nil {
		s.T().Logf("No .env file found")
	}

	if err := db.InitDB(); err != nil {
		s.T().Fatalf("Failed to initialize database: %v", err)
	}
}

func (s *ConcurrencyTestSuite) TearDownSuite() {
	db.CloseDB()
}

// SetupWallet creates or sets a wallet's balance
func (s *ConcurrencyTestSuite) SetupWallet(address, balance string) {
	_, err := db.DB.Exec("INSERT INTO wallets (address, balance) VALUES ($1, $2) ON CONFLICT (address) DO UPDATE SET balance = $2",
		address, balance)
	assert.NoError(s.T(), err)
}

// GetBalance gets a wallet's balance
func (s *ConcurrencyTestSuite) GetBalance(address string) string {
	var balance string
	err := db.DB.QueryRow("SELECT balance FROM wallets WHERE address = $1", address).Scan(&balance)
	assert.NoError(s.T(), err)
	return balance
}

// TestRaceConditions tests the race condition scenarios
func (s *ConcurrencyTestSuite) TestRaceConditions() {
	fromAddr := "0x0000000000000000000000000000000000000000"
	toAddr1 := "0x0000000000000000000000000000000000000001"
	toAddr2 := "0x0000000000000000000000000000000000000002"
	toAddr3 := "0x0000000000000000000000000000000000000003"

	// Reset balances
	s.SetupWallet(fromAddr, "10")
	s.SetupWallet(toAddr1, "0")
	s.SetupWallet(toAddr2, "0")
	s.SetupWallet(toAddr3, "0")

	var wg sync.WaitGroup
	wg.Add(3)

	var barrier = make(chan struct{})
	results := make([]error, 3)

	// Transfer 1: +1 token (credit)
	go func() {
		defer wg.Done()
		<-barrier                                                 // Wait for signal to start
		_, results[0] = db.TransferTokens(toAddr1, fromAddr, "1") // Note reversed from/to
	}()

	// Transfer 2: -4 tokens (debit)
	go func() {
		defer wg.Done()
		<-barrier // Wait for signal to start
		_, results[1] = db.TransferTokens(fromAddr, toAddr2, "4")
	}()

	// Transfer 3: -7 tokens (debit)
	go func() {
		defer wg.Done()
		<-barrier // Wait for signal to start
		_, results[2] = db.TransferTokens(fromAddr, toAddr3, "7")
	}()

	// Start all goroutines simultaneously
	close(barrier)
	wg.Wait()

	// Check the final balance
	finalBalance := s.GetBalance(fromAddr)

	// Count successful withdrawals
	withdrawalSuccessCount := 0
	for i, result := range results {
		if i == 0 {
			assert.NoError(s.T(), result, "Deposit should always succeed")
			continue
		}

		if result == nil {
			withdrawalSuccessCount++
		}
	}

	// Validate possible outcomes
	switch finalBalance {
	case "7":
		assert.Equal(s.T(), 1, withdrawalSuccessCount)
		assert.NoError(s.T(), results[1]) // -4
		assert.Error(s.T(), results[2])   // -7
		assert.Equal(s.T(), "insufficient balance", results[2].Error())

	case "4":
		assert.Equal(s.T(), 1, withdrawalSuccessCount)
		assert.Error(s.T(), results[1])   // -4
		assert.NoError(s.T(), results[2]) // -7
		assert.Equal(s.T(), "insufficient balance", results[1].Error())

	case "0":
		assert.Equal(s.T(), 2, withdrawalSuccessCount)
		assert.NoError(s.T(), results[1]) // -4
		assert.NoError(s.T(), results[2]) // -7

	default:
		s.T().Fatalf("Unexpected final balance: %s", finalBalance)
	}
}

// TestDeadlockPrevention tests that the system doesn't deadlock
func (s *ConcurrencyTestSuite) TestDeadlockPrevention() {
	wallet1 := "0xa000000000000000000000000000000000000001"
	wallet2 := "0xa000000000000000000000000000000000000002"

	s.SetupWallet(wallet1, "1000")
	s.SetupWallet(wallet2, "1000")

	const numTransfers = 20

	var wg sync.WaitGroup
	wg.Add(numTransfers * 2)

	errChan := make(chan error, numTransfers*2)
	var barrier = make(chan struct{})

	// Start bidirectional transfers
	for i := 0; i < numTransfers; i++ {
		go func() {
			defer wg.Done()
			<-barrier
			_, err := db.TransferTokens(wallet1, wallet2, "10")
			if err != nil {
				errChan <- err
			}
		}()

		go func() {
			defer wg.Done()
			<-barrier
			_, err := db.TransferTokens(wallet2, wallet1, "5")
			if err != nil {
				errChan <- err
			}
		}()
	}

	// Set timeout
	timeout := time.After(5 * time.Second)

	// Start transfers
	close(barrier)

	// Wait for completion
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.T().Log("All transfers completed without deadlock")
	case <-timeout:
		s.T().Fatal("Deadlock detected: transfers did not complete within timeout")
	}

	// Check for errors
	close(errChan)
	for err := range errChan {
		s.T().Errorf("Transfer error: %v", err)
	}

	// Verify final balances
	balance1 := s.GetBalance(wallet1)
	balance2 := s.GetBalance(wallet2)

	expected1 := 1000 - (10 * numTransfers) + (5 * numTransfers)
	expected2 := 1000 + (10 * numTransfers) - (5 * numTransfers)

	balance1Int, _ := new(big.Int).SetString(balance1, 10)
	balance2Int, _ := new(big.Int).SetString(balance2, 10)

	assert.Equal(s.T(), int64(expected1), balance1Int.Int64(), "Wallet1 balance incorrect")
	assert.Equal(s.T(), int64(expected2), balance2Int.Int64(), "Wallet2 balance incorrect")
}

// Run the concurrency test suite
func TestConcurrencySuite(t *testing.T) {
	suite.Run(t, new(ConcurrencyTestSuite))
}
