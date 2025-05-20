package unit

import (
	"database/sql"
	"errors"
	"math/big"
	"testing"
	"token-transfer-api/internal/db"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// WalletTestSuite defines the test suite structure
type WalletTestSuite struct {
	suite.Suite
	tx       *sql.Tx
	fromAddr string
	toAddr   string
}

// SetupSuite initializes the database connection
func (s *WalletTestSuite) SetupSuite() {
	if err := godotenv.Load("../../.env"); err != nil {
		s.T().Logf("No .env file found")
	}

	if err := db.InitDB(); err != nil {
		s.T().Fatalf("Failed to initialize database: %v", err)
	}
}

// TearDownSuite closes the database connection
func (s *WalletTestSuite) TearDownSuite() {
	db.CloseDB()
}

// SetupTest prepares the test environment before each test
func (s *WalletTestSuite) SetupTest() {
	var err error
	// Start a transaction for test isolation
	s.tx, err = db.DB.Begin()
	if err != nil {
		s.T().Fatalf("Failed to begin transaction: %v", err)
	}

	// Define test addresses
	s.fromAddr = "0x0000000000000000000000000000000000000000"
	s.toAddr = "0x0000000000000000000000000000000000000001"

	// Setup test state in transaction
	_, err = s.tx.Exec("UPDATE wallets SET balance = $1 WHERE address = $2", "1000000", s.fromAddr)
	assert.NoError(s.T(), err)

	_, err = s.tx.Exec("INSERT INTO wallets (address, balance) VALUES ($1, $2) ON CONFLICT (address) DO UPDATE SET balance = $2",
		s.toAddr, "0")
	assert.NoError(s.T(), err)
}

// TearDownTest cleans up after each test
func (s *WalletTestSuite) TearDownTest() {
	if s.tx != nil {
		s.tx.Rollback()
	}
}

// transferTokens is a helper method that executes a transfer within the test transaction
func (s *WalletTestSuite) transferTokens(from, to, amount string) (string, error) {
	// Validate amount
	amountBig, ok := new(big.Int).SetString(amount, 10)
	if !ok || amountBig.Cmp(big.NewInt(0)) <= 0 {
		return "", errors.New("invalid amount")
	}

	// Check sender balance
	var senderBalance string
	err := s.tx.QueryRow("SELECT balance FROM wallets WHERE address = $1", from).Scan(&senderBalance)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", errors.New("sender wallet does not exist")
		}
		return "", err
	}

	// Compare balance
	senderBalanceBig, _ := new(big.Int).SetString(senderBalance, 10)
	if senderBalanceBig.Cmp(amountBig) < 0 {
		return "", errors.New("insufficient balance")
	}

	// Calculate new balance
	newSenderBalance := new(big.Int).Sub(senderBalanceBig, amountBig)

	// Update sender
	_, err = s.tx.Exec("UPDATE wallets SET balance = $1 WHERE address = $2", newSenderBalance.String(), from)
	if err != nil {
		return "", err
	}

	// Check & update receiver
	var receiverExists bool
	err = s.tx.QueryRow("SELECT EXISTS(SELECT 1 FROM wallets WHERE address = $1)", to).Scan(&receiverExists)
	if err != nil {
		return "", err
	}

	if receiverExists {
		_, err = s.tx.Exec("UPDATE wallets SET balance = balance + $1 WHERE address = $2", amount, to)
	} else {
		_, err = s.tx.Exec("INSERT INTO wallets (address, balance) VALUES ($1, $2)", to, amount)
	}
	if err != nil {
		return "", err
	}

	// Record transfer
	_, err = s.tx.Exec("INSERT INTO transfers (from_address, to_address, amount) VALUES ($1, $2, $3)",
		from, to, amount)
	if err != nil {
		return "", err
	}

	return newSenderBalance.String(), nil
}

// createWallet is a helper to create a wallet with the given balance
func (s *WalletTestSuite) createWallet(address string, balance string) {
	_, err := s.tx.Exec("INSERT INTO wallets (address, balance) VALUES ($1, $2) ON CONFLICT (address) DO UPDATE SET balance = $2",
		address, balance)
	assert.NoError(s.T(), err)
}

// getBalance fetches the balance of a wallet
func (s *WalletTestSuite) getBalance(address string) string {
	var balance string
	err := s.tx.QueryRow("SELECT balance FROM wallets WHERE address = $1", address).Scan(&balance)
	assert.NoError(s.T(), err)
	return balance
}

// TestValidTransfer tests a basic valid transfer scenario
func (s *WalletTestSuite) TestValidTransfer() {
	balance, err := s.transferTokens(s.fromAddr, s.toAddr, "100")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "999900", balance)

	// Verify balances
	assert.Equal(s.T(), "999900", s.getBalance(s.fromAddr))
	assert.Equal(s.T(), "100", s.getBalance(s.toAddr))
}

// TestInsufficientBalance tests transfers with insufficient balance
func (s *WalletTestSuite) TestInsufficientBalance() {
	_, err := s.transferTokens(s.fromAddr, s.toAddr, "2000000")
	assert.Error(s.T(), err)
	assert.Equal(s.T(), "insufficient balance", err.Error())
}

// TestInvalidAmount tests transfers with invalid amounts
func (s *WalletTestSuite) TestInvalidAmount() {
	t := s.T()

	t.Run("Zero amount", func(t *testing.T) {
		_, err := s.transferTokens(s.fromAddr, s.toAddr, "0")
		assert.Error(t, err)
		assert.Equal(t, "invalid amount", err.Error())
	})

	t.Run("Negative amount", func(t *testing.T) {
		_, err := s.transferTokens(s.fromAddr, s.toAddr, "-100")
		assert.Error(t, err)
		assert.Equal(t, "invalid amount", err.Error())
	})
}

// TestNonExistentSender tests transfers from non-existent wallets
func (s *WalletTestSuite) TestNonExistentSender() {
	_, err := s.transferTokens("0xnonexistent", s.toAddr, "100")
	assert.Error(s.T(), err)
	assert.Equal(s.T(), "sender wallet does not exist", err.Error())
}

// TestTransferToNewWallet tests transfers to a new wallet
func (s *WalletTestSuite) TestTransferToNewWallet() {
	newAddr := "0x0000000000000000000000000000000000000002"

	// Perform transfer
	balance, err := s.transferTokens(s.fromAddr, newAddr, "200")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "999800", balance)

	// Verify new wallet was created with correct balance
	assert.Equal(s.T(), "200", s.getBalance(newAddr))
}

// TestTransferWithLargeAmounts tests transfers with large numbers
func (s *WalletTestSuite) TestTransferWithLargeAmounts() {
	largeAddr := "0x0000000000000000000000000000000000000003"
	largeBalance := "100000000000000000000000000000000000000000000000" // 10^47
	s.createWallet(largeAddr, largeBalance)

	// Transfer a large amount
	largeAmount := "10000000000000000000000000000000000000000000000" // 10^46
	balance, err := s.transferTokens(largeAddr, s.toAddr, largeAmount)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "90000000000000000000000000000000000000000000000", balance) // 9*10^46

	// Verify receiver got the large amount
	receiverBalance := s.getBalance(s.toAddr)

	receiverBig, ok := new(big.Int).SetString(receiverBalance, 10)
	assert.True(s.T(), ok)

	expectedBig, ok := new(big.Int).SetString("10000000000000000000000000000000000000000000000", 10) // 10^46
	assert.True(s.T(), ok)

	assert.Equal(s.T(), 0, receiverBig.Cmp(expectedBig))
}

// Run the suite
func TestWalletSuite(t *testing.T) {
	suite.Run(t, new(WalletTestSuite))
}
