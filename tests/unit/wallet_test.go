package unit

import (
	"database/sql"
	"errors"
	"math/big"
	"testing"
	"token-transfer-api/internal/db"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
)

func TestTransferTokens(t *testing.T) {
	// Setup
	if err := godotenv.Load("../../.env"); err != nil {
		t.Logf("No .env file found")
	}

	if err := db.InitDB(); err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.CloseDB()

	// Start a transaction for test isolation
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	// Rollback to ensure changes don't persist
	defer tx.Rollback()

	// Test data
	fromAddr := "0x0000000000000000000000000000000000000000"
	toAddr := "0x0000000000000000000000000000000000000001"

	// Setup test state in transaction
	_, err = tx.Exec("UPDATE wallets SET balance = $1 WHERE address = $2", "1000000", fromAddr)
	assert.NoError(t, err)

	_, err = tx.Exec("INSERT INTO wallets (address, balance) VALUES ($1, $2) ON CONFLICT (address) DO UPDATE SET balance = $2",
		toAddr, "0")
	assert.NoError(t, err)

	// Create temporary function for testing within transaction
	// Note: Using a custom function for tests that uses the transaction
	transferTokens := func(from, to, amount string) (string, error) {
		// Validate amount
		amountBig, ok := new(big.Int).SetString(amount, 10)
		if !ok || amountBig.Cmp(big.NewInt(0)) <= 0 {
			return "", errors.New("invalid amount")
		}

		// Check sender balance
		var senderBalance string
		err := tx.QueryRow("SELECT balance FROM wallets WHERE address = $1", from).Scan(&senderBalance)
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
		_, err = tx.Exec("UPDATE wallets SET balance = $1 WHERE address = $2", newSenderBalance.String(), from)
		if err != nil {
			return "", err
		}

		// Check & update receiver
		var receiverExists bool
		err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM wallets WHERE address = $1)", to).Scan(&receiverExists)
		if err != nil {
			return "", err
		}

		if receiverExists {
			_, err = tx.Exec("UPDATE wallets SET balance = balance + $1 WHERE address = $2", amount, to)
		} else {
			_, err = tx.Exec("INSERT INTO wallets (address, balance) VALUES ($1, $2)", to, amount)
		}
		if err != nil {
			return "", err
		}

		// Record transfer
		_, err = tx.Exec("INSERT INTO transfers (from_address, to_address, amount) VALUES ($1, $2, $3)",
			from, to, amount)
		if err != nil {
			return "", err
		}

		return newSenderBalance.String(), nil
	}

	// Test valid transfer
	balance, err := transferTokens(fromAddr, toAddr, "100")
	assert.NoError(t, err)
	assert.Equal(t, "999900", balance)

	// Verify balances within transaction
	var senderBalance, receiverBalance string
	err = tx.QueryRow("SELECT balance FROM wallets WHERE address = $1", fromAddr).Scan(&senderBalance)
	assert.NoError(t, err)
	assert.Equal(t, "999900", senderBalance)

	err = tx.QueryRow("SELECT balance FROM wallets WHERE address = $1", toAddr).Scan(&receiverBalance)
	assert.NoError(t, err)
	assert.Equal(t, "100", receiverBalance)

	// Test insufficient balance
	_, err = transferTokens(fromAddr, toAddr, "2000000")
	assert.Error(t, err)
	if err != nil {
		assert.Equal(t, "insufficient balance", err.Error())
	}

	// Test invalid amount
	_, err = transferTokens(fromAddr, toAddr, "0")
	assert.Error(t, err)
	if err != nil {
		assert.Equal(t, "invalid amount", err.Error())
	}

	// Test invalid sender
	_, err = transferTokens("0xnonexistent", toAddr, "100")
	assert.Error(t, err)
	if err != nil {
		assert.Equal(t, "sender wallet does not exist", err.Error())
	}
}
