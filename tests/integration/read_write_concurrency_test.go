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

type ReadWriteConcurrencySuite struct {
	suite.Suite
	server *httptest.Server
}

// SetupSuite initializes the test environment
func (s *ReadWriteConcurrencySuite) SetupSuite() {
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
func (s *ReadWriteConcurrencySuite) TearDownSuite() {
	s.server.Close()
	db.CloseDB()
}

// SetupTest resets the database state before each test
func (s *ReadWriteConcurrencySuite) SetupTest() {
	s.resetDBState()
}

// resetDBState resets the database to a known state
func (s *ReadWriteConcurrencySuite) resetDBState() {
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
func (s *ReadWriteConcurrencySuite) TearDownTest() {
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
func (s *ReadWriteConcurrencySuite) createWallet(address, balance string) {
	_, err := db.DB.Exec("INSERT INTO wallets (address, balance) VALUES ($1, $2) ON CONFLICT (address) DO UPDATE SET balance = $2",
		address, balance)
	assert.NoError(s.T(), err)
}

// getBalance gets a wallet's balance
func (s *ReadWriteConcurrencySuite) getBalance(address string) string {
	var balance string
	err := db.DB.QueryRow("SELECT balance FROM wallets WHERE address = $1", address).Scan(&balance)
	assert.NoError(s.T(), err)
	return balance
}

// executeTransfer makes a GraphQL request to transfer tokens
func (s *ReadWriteConcurrencySuite) executeTransfer(fromAddress, toAddress, amount string) (*graphQLResponse, error) {
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

// queryBalance makes a GraphQL request to query a wallet's balance
func (s *ReadWriteConcurrencySuite) queryBalance(address string) (string, error) {
	// Since the GraphQL schema doesn't have a query to get balance directly,
	// we'll query the database directly to simulate a balance query operation
	var balance string
	err := db.DB.QueryRow("SELECT balance FROM wallets WHERE address = $1", address).Scan(&balance)
	return balance, err
}

// TestParallelReadDuringWrite tests that reads during writes don't block and return consistent data
func (s *ReadWriteConcurrencySuite) TestParallelReadDuringWrite() {
	fromAddr := "0x0000000000000000000000000000000000000000"
	toAddr := "0x0000000000000000000000000000000000000001"

	// Set initial balance for test
	s.createWallet(fromAddr, "1000")
	s.createWallet(toAddr, "0")

	const numReaders = 10
	const numWriters = 5
	const readsPerReader = 20

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(numReaders + numWriters)

	// Launch multiple reader goroutines
	for i := 0; i < numReaders; i++ {
		go func(readerID int) {
			defer wg.Done()
			for j := 0; j < readsPerReader; j++ {
				select {
				case <-ctx.Done():
					return
				default:
					balance, err := s.queryBalance(fromAddr)
					assert.NoError(s.T(), err, "Reader %d, read %d failed", readerID, j)
					// Just verify balance is a non-empty string
					assert.NotEmpty(s.T(), balance, "Reader %d, read %d got empty balance", readerID, j)

					// Small delay to simulate processing
					time.Sleep(time.Millisecond * 5)
				}
			}
		}(i)
	}

	// Launch multiple writer goroutines performing transfers
	for i := 0; i < numWriters; i++ {
		go func(writerID int) {
			defer wg.Done()
			transferAmount := fmt.Sprintf("%d", (writerID+1)*10) // Different amounts

			result, err := s.executeTransfer(fromAddr, toAddr, transferAmount)
			assert.NoError(s.T(), err, "Writer %d transfer failed", writerID)

			// Verify transfer result contains something
			if result != nil && result.Errors != nil {
				// Only log failures when balance is insufficient (which may happen in later transfers)
				for _, errMap := range result.Errors {
					s.T().Logf("Writer %d transfer error: %v", writerID, errMap["message"])
				}
			}
		}(i)
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.T().Log("All parallel reads and writes completed without deadlock")
	case <-ctx.Done():
		s.T().Fatal("Deadlock detected: operations did not complete within timeout")
	}

	// Final check - verify the system is in a consistent state
	fromBalance := s.getBalance(fromAddr)
	toBalance := s.getBalance(toAddr)

	s.T().Logf("Final balances - From: %s, To: %s", fromBalance, toBalance)

	// We can't predict exact balances due to concurrent transfers, but we can verify:
	// 1. Balances are non-empty
	assert.NotEmpty(s.T(), fromBalance)
	assert.NotEmpty(s.T(), toBalance)

	// 2. The sum of balances equals the initial amount (1000)
	// We need to handle these as integers for proper comparison
	var fromInt, toInt int
	fmt.Sscanf(fromBalance, "%d", &fromInt)
	fmt.Sscanf(toBalance, "%d", &toInt)
	assert.Equal(s.T(), 1000, fromInt+toInt, "Sum of balances should equal the initial amount")
}

// TestInterleavedReadWrites tests behavior when reads and writes are highly interleaved
func (s *ReadWriteConcurrencySuite) TestInterleavedReadWrites() {
	mainWallet := "0x0000000000000000000000000000000000000000"
	targetWallet := "0x0000000000000000000000000000000000000001"

	// Set initial balance
	s.createWallet(mainWallet, "500")
	s.createWallet(targetWallet, "0")

	var wg sync.WaitGroup
	const iterations = 20
	wg.Add(iterations)

	// Use channel to collect all balance records seen during the test
	balanceLog := make(chan string, iterations*2)
	transferSuccessLog := make(chan bool, iterations)
	transferAmountLog := make(chan int, iterations)

	// Run multiple operations that each do a read followed by a write
	for i := 0; i < iterations; i++ {
		go func(i int) {
			defer wg.Done()

			// First read the balance
			balance, err := s.queryBalance(mainWallet)
			assert.NoError(s.T(), err)
			balanceLog <- balance

			// Small random amount (1-10 tokens)
			amount := (i % 10) + 1
			transferAmountLog <- amount

			// Then immediately try to transfer some tokens
			result, err := s.executeTransfer(mainWallet, targetWallet, fmt.Sprintf("%d", amount))
			assert.NoError(s.T(), err)

			// Log if transfer was successful or failed
			if result != nil && result.Errors == nil {
				transferSuccessLog <- true
			} else {
				transferSuccessLog <- false
			}

			// Read balance again after transfer
			balance, err = s.queryBalance(mainWallet)
			assert.NoError(s.T(), err)
			balanceLog <- balance
		}(i)
	}

	// Wait for all operations to complete
	wg.Wait()
	close(balanceLog)
	close(transferSuccessLog)
	close(transferAmountLog)

	// Collect and analyze the results
	var balances []string
	for balance := range balanceLog {
		balances = append(balances, balance)
	}

	var successCount int
	for success := range transferSuccessLog {
		if success {
			successCount++
		}
	}

	var totalTransferAmount int
	for amount := range transferAmountLog {
		totalTransferAmount += amount
	}

	s.T().Logf("Observed %d balances during interleaved operations", len(balances))
	s.T().Logf("Successful transfers: %d/%d", successCount, iterations)
	s.T().Logf("Total transfer amount attempted: %d", totalTransferAmount)

	// Final balance check - should reflect exactly the amount successfully transferred
	finalFromBalance := s.getBalance(mainWallet)
	finalToBalance := s.getBalance(targetWallet)

	var fromInt, toInt int
	fmt.Sscanf(finalFromBalance, "%d", &fromInt)
	fmt.Sscanf(finalToBalance, "%d", &toInt)

	s.T().Logf("Final balances - From: %s, To: %s", finalFromBalance, finalToBalance)
	assert.Equal(s.T(), 500, fromInt+toInt, "Total tokens should be preserved")
	assert.Equal(s.T(), successCount, toInt, "To wallet should have exactly the number of successful transfers")
}

// TestReadDuringMultiWalletTransfers tests reads during complex multi-wallet transfers
func (s *ReadWriteConcurrencySuite) TestReadDuringMultiWalletTransfers() {
	// Create several wallets with different balances
	wallets := []struct {
		address string
		balance string
	}{
		{"0xc000000000000000000000000000000000000001", "100"},
		{"0xc000000000000000000000000000000000000002", "200"},
		{"0xc000000000000000000000000000000000000003", "300"},
		{"0xc000000000000000000000000000000000000004", "400"},
	}

	// Setup the wallets
	for _, wallet := range wallets {
		s.createWallet(wallet.address, wallet.balance)
	}

	var wg sync.WaitGroup
	const (
		numReaders     = 5
		numTransfers   = 20
		readsPerReader = 15
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Run multiple readers that continuously read all wallet balances
	wg.Add(numReaders)
	for r := 0; r < numReaders; r++ {
		go func(readerID int) {
			defer wg.Done()

			for i := 0; i < readsPerReader; i++ {
				select {
				case <-ctx.Done():
					return
				default:
					// Read all wallet balances
					balanceSum := 0
					for _, wallet := range wallets {
						balance, err := s.queryBalance(wallet.address)
						assert.NoError(s.T(), err)

						var balanceInt int
						fmt.Sscanf(balance, "%d", &balanceInt)
						balanceSum += balanceInt
					}

					assert.Equal(s.T(), 1000, balanceSum, "Reader %d, read %d: Total balance changed", readerID, i)

					// Small delay
					time.Sleep(time.Millisecond * 2)
				}
			}
		}(r)
	}

	// Run multiple transfers between wallets in a round-robin fashion
	wg.Add(numTransfers)
	for t := 0; t < numTransfers; t++ {
		go func(transferID int) {
			defer wg.Done()

			// Round-robin: each wallet transfers to the next wallet
			fromIndex := transferID % len(wallets)
			toIndex := (fromIndex + 1) % len(wallets)

			from := wallets[fromIndex].address
			to := wallets[toIndex].address
			amount := fmt.Sprintf("%d", (transferID%10)+1) // Amount 1-10

			select {
			case <-ctx.Done():
				return
			default:
				result, err := s.executeTransfer(from, to, amount)
				assert.NoError(s.T(), err, "Transfer %d failed with error", transferID)

				if result != nil && result.Errors != nil {
					s.T().Logf("Transfer %d from %s to %s (amount %s) error: %v",
						transferID, from, to, amount, result.Errors[0]["message"])
				}
			}
		}(t)
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.T().Log("All parallel operations completed successfully")
	case <-ctx.Done():
		s.T().Fatal("Deadlock detected: operations did not complete within timeout")
	}

	// Final verification - sum of all balances should remain the same
	var finalSum int
	for _, wallet := range wallets {
		balance := s.getBalance(wallet.address)
		var balanceInt int
		fmt.Sscanf(balance, "%d", &balanceInt)
		finalSum += balanceInt
	}

	assert.Equal(s.T(), 1000, finalSum, "Total balance should remain 1000 after all transfers")
}

// Run the parallel read/write suite
func TestReadWriteConcurrencySuite(t *testing.T) {
	suite.Run(t, new(ReadWriteConcurrencySuite))
}
