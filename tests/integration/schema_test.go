package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"token-transfer-api/internal/db"
	"token-transfer-api/pkg/graphql"

	"net/http/httptest"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type SchemaTestSuite struct {
	suite.Suite
	server *httptest.Server
}

// SetupSuite initializes the test environment
func (s *SchemaTestSuite) SetupSuite() {
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
func (s *SchemaTestSuite) TearDownSuite() {
	s.server.Close()
	db.CloseDB()
}

// TestGraphQLSchemaValidation tests that the schema is correctly defined
func (s *SchemaTestSuite) TestGraphQLSchemaValidation() {
	// Query the schema introspection
	introspectionQuery := `{
		__schema {
			types {
				name
				kind
				fields {
					name
					type {
						name
						kind
						ofType {
							name
							kind
						}
					}
					args {
						name
						type {
							kind
							name
							ofType {
								kind
								name
							}
						}
					}
				}
			}
		}
	}`

	reqBody, _ := json.Marshal(graphQLRequest{Query: introspectionQuery})
	resp, err := http.Post(s.server.URL, "application/json", bytes.NewBuffer(reqBody))
	assert.NoError(s.T(), err)
	defer resp.Body.Close()

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	assert.NoError(s.T(), err)

	data, hasData := result["data"].(map[string]interface{})
	assert.True(s.T(), hasData, "Response should contain data")

	// Validate schema structure
	schema, hasSchema := data["__schema"].(map[string]interface{})
	assert.True(s.T(), hasSchema, "Schema data should be present")

	types, hasTypes := schema["types"].([]interface{})
	assert.True(s.T(), hasTypes, "Schema should have types")

	// Find the Mutation type
	var mutationType map[string]interface{}
	for _, t := range types {
		typeObj, isObj := t.(map[string]interface{})
		if !isObj {
			continue
		}

		name, hasName := typeObj["name"].(string)
		if !hasName {
			continue
		}

		if name == "Mutation" {
			mutationType = typeObj
			break
		}
	}

	assert.NotNil(s.T(), mutationType, "Mutation type should be defined in schema")

	// Check if transfer mutation exists with required arguments
	fields, hasFields := mutationType["fields"].([]interface{})
	assert.True(s.T(), hasFields, "Mutation should have fields")

	var transferField map[string]interface{}
	for _, f := range fields {
		field, isObj := f.(map[string]interface{})
		if !isObj {
			continue
		}

		name, hasName := field["name"].(string)
		if !hasName {
			continue
		}

		if name == "transfer" {
			transferField = field
			break
		}
	}

	assert.NotNil(s.T(), transferField, "transfer mutation should be defined")

	// Validate transfer mutation arguments
	args, hasArgs := transferField["args"].([]interface{})
	assert.True(s.T(), hasArgs, "transfer mutation should have arguments")
	assert.Equal(s.T(), 3, len(args), "transfer should have exactly 3 arguments")

	// Map to check if all required arguments exist
	requiredArgs := map[string]bool{
		"from_address": false,
		"to_address":   false,
		"amount":       false,
	}

	// Check each argument
	for _, a := range args {
		arg, isObj := a.(map[string]interface{})
		if !isObj {
			continue
		}

		name, hasName := arg["name"].(string)
		if !hasName {
			continue
		}

		// Mark argument as found
		if _, exists := requiredArgs[name]; exists {
			requiredArgs[name] = true
		}

		// Check that the argument is non-nullable
		argType, hasType := arg["type"].(map[string]interface{})
		assert.True(s.T(), hasType, "Argument should have a type")

		kind, hasKind := argType["kind"].(string)
		assert.True(s.T(), hasKind, "Type should have a kind")

		// Either the type itself is NON_NULL or its ofType should be
		if kind == "NON_NULL" {
			ofType, hasOfType := argType["ofType"].(map[string]interface{})
			assert.True(s.T(), hasOfType, "NON_NULL type should have ofType")

			ofTypeName, hasName := ofType["name"].(string)
			assert.True(s.T(), hasName, "ofType should have a name")

			// For string arguments
			assert.Equal(s.T(), "String", ofTypeName, "Arguments should be of String type")
		} else {
			s.T().Errorf("Argument %s should be NON_NULL", name)
		}
	}

	// Verify all required arguments were found
	for arg, found := range requiredArgs {
		assert.True(s.T(), found, "Required argument %s not found in schema", arg)
	}

	// Validate transfer return type
	returnType, hasType := transferField["type"].(map[string]interface{})
	assert.True(s.T(), hasType, "transfer should have a return type")

	typeName, hasName := returnType["name"].(string)
	if !hasName {
		ofType, hasOfType := returnType["ofType"].(map[string]interface{})
		if hasOfType {
			typeName, hasName = ofType["name"].(string)
		}
	}

	assert.True(s.T(), hasName, "Return type should have a name")
	assert.Equal(s.T(), "TransferResult", typeName, "transfer should return TransferResult type")
}

// Run the schema test suite
func TestSchemaTestSuite(t *testing.T) {
	suite.Run(t, new(SchemaTestSuite))
}
