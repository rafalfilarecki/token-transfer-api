# Token Transfer API

A GraphQL API for transferring BTP tokens between wallets, built with Go.

## Overview

This API allows transferring BTP tokens between wallets. Initially, there is a default wallet (address: `0x0000000000000000000000000000000000000000`) holding 1,000,000 BTP tokens. The API supports transferring tokens from one wallet to another with proper handling of race conditions.

## Features

- GraphQL API with a single `transfer` mutation
- Transaction-based token transfers
- PostgreSQL database for storing wallet balances and transfer history
- Dockerized environment for easy setup

## Prerequisites

- Go 1.22 or higher
- Docker and Docker Compose

## Project Structure

```
token-transfer-api/
├── cmd/api/         # Application entry point
├── internal/        # Internal packages
│   ├── db/          # Database operations
│   ├── graph/       # GraphQL resolvers
│   └── model/       # Data models
├── pkg/             # Reusable components
│   └── graphql/     # GraphQL schema and handler
├── tests/           # Test suites
│   ├── integration/ # Integration tests
│   └── unit/        # Unit tests
├── sql/             # SQL scripts
├── docker-compose.yaml
└── Makefile
```

## Setup

1. Clone the repository
2. Create a `.env` file from the template:
   ```
   cp .env.template .env
   ```
3. Start the PostgreSQL database:
   ```
   make db-up
   ```
4. Verify database health:
   ```
   make db-health
   ```
5. Install dependencies:
   ```
   make deps
   ```

## Running the Application

To start the API server:

```
make run
```

The GraphQL API will be available at `http://localhost:8080/query`.

## Testing

Run all tests:
```
make test
```

Run unit tests only:
```
make test-unit
```

Run integration tests only:
```
make test-integration
```

## API Usage

### Transfer Mutation

Transfer tokens between wallets:

```graphql
mutation {
  transfer(
    from_address: "0x0000000000000000000000000000000000000000", 
    to_address: "0x0000000000000000000000000000000000000001", 
    amount: "100"
  ) {
    balance
  }
}
```

Response:
```json
{
  "data": {
    "transfer": {
      "balance": "999900"
    }
  }
}
```

## Database Schema

### Wallets Table
- `address`: Wallet address (VARCHAR, PRIMARY KEY)
- `balance`: Token balance (DECIMAL)
- `created_at`: Creation timestamp
- `updated_at`: Last update timestamp

### Transfers Table
- `id`: Transfer ID (SERIAL, PRIMARY KEY)
- `from_address`: Sender address (FK to wallets)
- `to_address`: Receiver address (FK to wallets)
- `amount`: Transfer amount (DECIMAL)
- `created_at`: Creation timestamp