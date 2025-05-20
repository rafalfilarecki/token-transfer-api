package graphql

import (
	"encoding/json"
	"io"
	"net/http"
	"token-transfer-api/internal/graph"

	"github.com/graphql-go/graphql"
)

type GraphQLRequest struct {
	Query         string                 `json:"query"`
	OperationName string                 `json:"operationName,omitempty"`
	Variables     map[string]interface{} `json:"variables,omitempty"`
}

func NewHandler() http.Handler {
	schema, err := createSchema()
	if err != nil {
		panic(err)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusOK)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}

		var req GraphQLRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "Error parsing request body", http.StatusBadRequest)
			return
		}

		result := executeQuery(schema, req.Query, req.Variables)
		json.NewEncoder(w).Encode(result)
	})
}

func executeQuery(schema graphql.Schema, query string, variables map[string]interface{}) *graphql.Result {
	return graphql.Do(graphql.Params{
		Schema:         schema,
		RequestString:  query,
		VariableValues: variables,
	})
}

func createSchema() (graphql.Schema, error) {
	resolver := &graph.Resolver{}

	walletType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Wallet",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.String,
			},
			"balance": &graphql.Field{
				Type: graphql.String,
			},
		},
	})

	transferResultType := graphql.NewObject(graphql.ObjectConfig{
		Name: "TransferResult",
		Fields: graphql.Fields{
			"balance": &graphql.Field{
				Type: graphql.String,
			},
		},
	})

	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"wallet": &graphql.Field{
				Type: walletType,
				Args: graphql.FieldConfigArgument{
					"address": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.String),
					},
				},
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					address := p.Args["address"].(string)
					return resolver.GetWallet(address)
				},
			},
		},
	})

	mutationType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Mutation",
		Fields: graphql.Fields{
			"transfer": &graphql.Field{
				Type: transferResultType,
				Args: graphql.FieldConfigArgument{
					"from_address": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.String),
					},
					"to_address": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.String),
					},
					"amount": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.String),
					},
				},
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					args := graph.TransferArgs{
						FromAddress: p.Args["from_address"].(string),
						ToAddress:   p.Args["to_address"].(string),
						Amount:      p.Args["amount"].(string),
					}
					return resolver.Transfer(args)
				},
			},
		},
	})

	return graphql.NewSchema(graphql.SchemaConfig{
		Query:    queryType,
		Mutation: mutationType,
	})
}
