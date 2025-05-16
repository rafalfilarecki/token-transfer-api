package graph

import (
	"token-transfer-api/internal/db"
	"token-transfer-api/internal/model"
)

type Resolver struct{}

type TransferArgs struct {
	FromAddress string `json:"from_address"`
	ToAddress   string `json:"to_address"`
	Amount      string `json:"amount"`
}

func (r *Resolver) Transfer(args TransferArgs) (*model.TransferResult, error) {
	balance, err := db.TransferTokens(args.FromAddress, args.ToAddress, args.Amount)
	if err != nil {
		return nil, err
	}

	return &model.TransferResult{
		Balance: balance,
	}, nil
}
