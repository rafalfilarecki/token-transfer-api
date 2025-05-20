package model

type Wallet struct {
	Address string `json:"address"`
	Balance string `json:"balance"`
}

type TransferResult struct {
	Balance string `json:"balance"`
}
