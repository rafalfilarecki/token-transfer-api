package db

import (
	"database/sql"
	"errors"
	"math/big"
	"token-transfer-api/internal/model"
)

func GetWallet(address string) (*model.Wallet, error) {
	var wallet model.Wallet
	err := DB.QueryRow("SELECT address, balance FROM wallets WHERE address = $1", address).Scan(&wallet.Address, &wallet.Balance)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &wallet, nil
}

func TransferTokens(fromAddress, toAddress, amount string) (string, error) {
	amountBig := new(big.Int)
	_, ok := amountBig.SetString(amount, 10)
	if !ok || amountBig.Cmp(big.NewInt(0)) <= 0 {
		return "", errors.New("invalid amount")
	}

	tx, err := DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var senderBalance string
	err = tx.QueryRow("SELECT balance FROM wallets WHERE address = $1", fromAddress).Scan(&senderBalance)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", errors.New("sender wallet does not exist")
		}
		return "", err
	}

	senderBalanceBig := new(big.Int)
	_, ok = senderBalanceBig.SetString(senderBalance, 10)
	if !ok {
		return "", errors.New("invalid sender balance format")
	}

	if senderBalanceBig.Cmp(amountBig) < 0 {
		return "", errors.New("insufficient balance")
	}

	newSenderBalance := new(big.Int).Sub(senderBalanceBig, amountBig)

	_, err = tx.Exec("UPDATE wallets SET balance = $1 WHERE address = $2", newSenderBalance.String(), fromAddress)
	if err != nil {
		return "", err
	}

	var receiverExists bool
	err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM wallets WHERE address = $1)", toAddress).Scan(&receiverExists)
	if err != nil {
		return "", err
	}

	if receiverExists {
		_, err = tx.Exec("UPDATE wallets SET balance = balance + $1 WHERE address = $2", amount, toAddress)
	} else {
		_, err = tx.Exec("INSERT INTO wallets (address, balance) VALUES ($1, $2)", toAddress, amount)
	}
	if err != nil {
		return "", err
	}

	_, err = tx.Exec("INSERT INTO transfers (from_address, to_address, amount) VALUES ($1, $2, $3)",
		fromAddress, toAddress, amount)
	if err != nil {
		return "", err
	}

	if err = tx.Commit(); err != nil {
		return "", err
	}

	return newSenderBalance.String(), nil
}
