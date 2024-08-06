package main

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"

	"fmt"
	"log"
	"math/big"
	"os"
	"regexp"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/gofiber/contrib/swagger"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

var RPC_URL string
var rpcClient *rpc.Client

// A Block hash is 32 bytes long and hence 64 characters long plus 0x prefix
var hashRegex = regexp.MustCompile(`^0x[0-9a-f]{64}$`)

// A block number also allows default block identifiers such as "earliest", "latest" and "pending"
// A block number can also be a decimal number without 0x prefix (part of my proposal) - defined by decimalNumberRegex
// A block number can also be a hex number with 0x prefix
// A block number will always consist of a non-zero character after 0x, except for "0x0".

var blockNumberRegex = regexp.MustCompile(`^0x([1-9a-f]+[0-9a-f]*|0)$`)
var decimalNumberRegex = regexp.MustCompile(`^([1-9][0-9]*|0)$`)
var defaultBlockParamRegex = regexp.MustCompile(`^(earliest|latest|pending|safe|finalized)$`)

// request body for eth_call
type RequestBody struct {
	From     common.Address  `json:"from,omitempty" xml:"from" form:"from"`
	To       *common.Address `json:"to" xml:"to" form:"to" validate:"required"`
	Gas      string          `json:"gas,omitempty" xml:"gas" form:"gas"`
	GasPrice string          `json:"gasPrice,omitempty" xml:"gasPrice" form:"gasPrice"`
	Value    string          `json:"value,omitempty" xml:"value" form:"value"`
	Input    string          `json:"data" xml:"data" form:"input" validate:"required"`
}

// request body for sending transactions
type ModifiedRequestBody struct {
	RequestBody
	From  common.Address  `json:"from" xml:"from" form:"from" validate:"required"`
	To    *common.Address `json:"to,omitempty" xml:"to" form:"to"`
	Nonce string          `json:"nonce,omitempty"`
}

type rpcTransaction struct {
	BlockHash        *common.Hash    `json:"blockHash,omitempty"`
	BlockNumber      string          `json:"blockNumber,omitempty"`
	From             *common.Address `json:"from,omitempty"`
	Gas              string          `json:"gas,omitempty"`
	GasPrice         string          `json:"gasPrice,omitempty"`
	Hash             *common.Hash    `json:"hash,omitempty"`
	Input            *string         `json:"input,omitempty"`
	Nonce            string          `json:"nonce,omitempty"`
	To               *common.Address `json:"to,omitempty"`
	TransactionIndex string          `json:"transactionIndex,omitempty"`
	Value            string          `json:"value,omitempty"`
	V                string          `json:"v,omitempty"`
	R                *string         `json:"r,omitempty"`
	S                *string         `json:"s,omitempty"`
}

type txReceipt struct {
	TxHash            *common.Hash    `json:"transactionHash,omitempty"`
	TxIndex           string          `json:"transactionIndex,omitempty"`
	BlockHash         *common.Hash    `json:"blockHash,omitempty"`
	BlockNumber       string          `json:"blockNumber,omitempty"`
	From              *common.Address `json:"from,omitempty"`
	To                *common.Address `json:"to,omitempty"`
	CumulativeGasUsed string          `json:"cumulativeGasUsed,omitempty"`
	EffectiveGasPrice string          `json:"effectiveGasPrice,omitempty"`
	GasUsed           string          `json:"gasUsed,omitempty"`
	ContractAddress   *common.Address `json:"contractAddress,omitempty"`
	Logs              []types.Log     `json:"logs,omitempty"`
	Bloom             *types.Bloom    `json:"logsBloom,omitempty"`
	Type              string          `json:"type,omitempty"`
	Root              *common.Hash    `json:"root,omitempty"`
	Status            string          `json:"status,omitempty"`
}

func main() {
	app := fiber.New()
	app.Use(recover.New())

	cfg := swagger.Config{
		BasePath: "/",
		FilePath: "./openapi.json",
		Path:     "docs",
		Title:    "Ethereum RPC API",
		CacheAge: 3600,
	}
	app.Use(swagger.New(cfg))

	if len(os.Args) < 2 {
		log.Fatal("Usage: ./ethRPCtoREST <rpc_url>")
	}
	RPC_URL = os.Args[1]
	log.Println(RPC_URL)

	var err error

	rpcClient, err = rpc.Dial(RPC_URL)
	if err != nil {
		log.Fatal(err)
	}

	// History Methods
	app.Get("/eth/block/:identifier", getBlockByIdentifier)
	app.Get("/eth/tx/:hash", getTransactionByHash)
	app.Get("/eth/tx/block/:identifier/:index", getTransactionByIdentifierAndIndex)
	app.Get("/eth/tx/receipt/:hash", getTransactionReceiptByHash)
	app.Get("/eth/uncle/block/:identifier/:index", getUncleByBlockIdentifierAndIndex)
	app.Get("/eth/unclecount/block/:identifier", getUncleCountByBlockIdentifier)
	app.Get("/eth/block/txcount/:identifier", getBlockTransactionCountByIdentifier)

	// Gossip Methods
	app.Get("/eth/blockNumber", getBlockNumber)
	app.Post("/eth/tx/:data", sendRawTransaction)
	app.Post("/eth/tx", sendTransaction)

	// State Methods
	app.Get("/eth/balance/:address", getBalanceOfAddressAtBlock) // default block parameter is "latest"
	app.Get("/eth/balance/:address/:identifier", getBalanceOfAddressAtBlock)
	app.Get("/eth/storage/:address/:position", getStorageAtAddressAndPositionAtBlock) // default block parameter is "latest
	app.Get("/eth/storage/:address/:position/:identifier", getStorageAtAddressAndPositionAtBlock)
	app.Get("/eth/txcount/:address", getTransactionCountOfAddressAtBlock) // default block parameter is "latest"
	app.Get("/eth/txcount/:address/:identifier", getTransactionCountOfAddressAtBlock)
	app.Get("/eth/code/:address", getCodeOfAddressAtBlock) // default block parameter is "latest"
	app.Get("/eth/code/:address/:identifier", getCodeOfAddressAtBlock)
	app.Post("/eth/call", callContractAtBlock) // default block parameter is "latest"
	app.Post("/eth/call/:identifier", callContractAtBlock)
	app.Post("/eth/estimategas", estimateGas) // default block parameter is "latest"

	// Utils endpoint
	app.Post("/eth/encode", encodeFunctionSignature)

	// TODO:(very low priority) also support shortform apis like /e/b/:identifier, /e/t/:hash, /e/t/b/:identifier/:index, /e/t/r/:hash, /e/u/b/:identifier/:index, /e/uc/b/:identifier
	// TODO: I have an idea that is I will make docs of APIs also on the same server. Docs will came up in conditions like:
	// - when user will hit the server on non-existent route.
	// - when user will hit the server with a route matching the routes of APIs but without any query params. like /eth/block or /eth/transaction these will consist of usage of APIs and expected query params for that route specifically.
	// - One thing to keep in mind that it might be an inconvenient thing for the user to get docs on path with wrong query params. So, make sure to check if the request consists of header "Accept: application/json" or not. If it does then return JSON response else return HTML response.
	log.Fatal(app.Listen(":3000"))
}

// Handlers
func encodeFunctionSignature(c *fiber.Ctx) error {
	// Parse the request body
	obj := new(struct {
		Data string `json:"data" xml:"data" form:"data"`
	})

	if err := c.BodyParser(obj); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Encode the function signature
	encodedInput, err := EncodeFunctionSignature(obj.Data)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Return the encoded input
	return c.JSON(fiber.Map{
		"encodedInput": encodedInput,
	})
}

// getBlockByIdentifier retrieves block information by block hash or block number and returns it as JSON.
func getBlockByIdentifier(c *fiber.Ctx) error {
	identifier := c.Params("identifier")
	includeTx := c.QueryBool("includeTx")

	// Check if identifier is a block hash or block number
	if hashRegex.MatchString(identifier) {
		log.Println("Block hash")
		blockInfo := getBlockByHash(c, identifier, includeTx)
		return blockInfo
	} else if blockNumberRegex.MatchString(identifier) || defaultBlockParamRegex.MatchString(identifier) || decimalNumberRegex.MatchString(identifier) {
		log.Println("Block number")
		blockInfo := getBlockByNumber(c, identifier, includeTx)
		return blockInfo
	} else {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid identifier",
		})
	}
}

// getBlockByHash retrieves block information by hash and returns it as JSON.
func getBlockByHash(c *fiber.Ctx, hash string, includeTx bool) error {
	var ctx = context.Background()
	var blockInfo *types.Header
	err := rpcClient.CallContext(ctx, &blockInfo, "eth_getBlockByHash", hash, includeTx)
	if err != nil {
		log.Print("Error fetching block info:", err)
	}
	return c.JSON(StringifyHeader(blockInfo))
}

// getBlockByNumber retrieves block information by block number or default block parameters and returns it as JSON.
func getBlockByNumber(c *fiber.Ctx, numberOrDefaultParameters string, includeTx bool) error {
	if decimalNumberRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Decimal number")
		blockInfo := getBlockByDecimalNumber(c, numberOrDefaultParameters, includeTx)
		return blockInfo
	} else {
		number := numberOrDefaultParameters
		log.Println(number)

		if !defaultBlockParamRegex.MatchString(number) {
			// verifying if a valid kind of hex is provided
			blockNumber, success := new(big.Int).SetString(number[2:], 16)
			if !success {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "Invalid block number",
				})
			}
			log.Println(blockNumber)
		}

		var ctx = context.Background()
		var blockInfo *types.Header

		err := rpcClient.CallContext(ctx, &blockInfo, "eth_getBlockByNumber", number, includeTx)
		if err != nil {
			log.Print("Error fetching block info:", err)
		}
		return c.JSON(StringifyHeader(blockInfo))
	}
}

func getBlockByDecimalNumber(c *fiber.Ctx, number string, includeTx bool) error {
	hexNumber := decimalToHex(number)
	if hexNumber == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid block number " + number,
		})
	}

	var ctx = context.Background()
	var blockInfo *types.Header

	err := rpcClient.CallContext(ctx, &blockInfo, "eth_getBlockByNumber", hexNumber, includeTx)
	if err != nil {
		log.Print("Error fetching block info:", err)
	}
	return c.JSON(StringifyHeader(blockInfo))
}

func getTransactionByHash(c *fiber.Ctx) error {
	hash := c.Params("hash")
	// Check if hash is a valid transaction hash
	if hashRegex.MatchString(hash) {
		var ctx = context.Background()
		var transaction *rpcTransaction
		err := rpcClient.CallContext(ctx, &transaction, "eth_getTransactionByHash", hash)
		if err != nil {
			log.Print("Error fetching transaction info:", err)
		}
		return c.JSON(
			StringifyTransaction(
				transaction))
	} else {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid transaction hash",
		})
	}
}

func getTransactionByIdentifierAndIndex(c *fiber.Ctx) error {
	identifier := c.Params("identifier")
	index := c.Params("index") // hex or decimal string
	log.Print(identifier)
	log.Print(index)
	var indexNumber big.Int
	if blockNumberRegex.MatchString(index) {
		// Check if index is a valid number by converting hex to decimal
		indexNumberTemp, success := new(big.Int).SetString(index[2:], 16)
		if !success {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid index",
			})
		}
		indexNumber = *indexNumberTemp
	} else if decimalNumberRegex.MatchString(index) {
		// index is supposed to be a hex string
		index = decimalToHex(index)
		if index == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid index " + index,
			})
		}
	} else {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid index",
		})
	}

	// Check if identifier is a block hash or block number
	if hashRegex.MatchString(identifier) {
		log.Println("Block hash")
		blockInfo := getTransactionByBlockHashAndIndex(c, identifier, uint(indexNumber.Uint64()))
		return blockInfo
	} else if blockNumberRegex.MatchString(identifier) || defaultBlockParamRegex.MatchString(identifier) || decimalNumberRegex.MatchString(identifier) {
		log.Println("Block number")
		blockInfo := getTransactionByBlockNumberAndIndex(c, identifier, index)
		return blockInfo
	} else {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid identifier",
		})
	}
}

func getTransactionByBlockNumberAndIndex(c *fiber.Ctx, numberOrDefaultParameters string, index string) error {

	var json *rpcTransaction

	if decimalNumberRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Decimal number")
		hexNumber := decimalToHex(numberOrDefaultParameters)
		if hexNumber == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid number " + numberOrDefaultParameters,
			})
		}

		var ctx = context.Background()
		err := rpcClient.CallContext(ctx, &json, "eth_getTransactionByBlockNumberAndIndex", hexNumber, index)
		if err != nil {
			log.Print("Error fetching transaction info:", err)
		}
		return c.JSON(StringifyTransaction(json))

	} else {
		number := numberOrDefaultParameters
		log.Println(number)

		if !defaultBlockParamRegex.MatchString(number) {
			blockNumber, success := new(big.Int).SetString(number[2:], 16)
			if !success {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "Invalid number",
				})
			}
			log.Println(blockNumber)
		}
		var ctx = context.Background()
		err := rpcClient.CallContext(ctx, &json, "eth_getTransactionByBlockNumberAndIndex", number, index)
		if err != nil {
			log.Print("Error fetching transaction info:", err)
		}
		return c.JSON(StringifyTransaction(json))
	}
}

func getTransactionByBlockHashAndIndex(c *fiber.Ctx, hash string, index uint) error {
	blockHash := common.HexToHash(hash)
	log.Println(blockHash)
	var ctx = context.Background()
	var json *rpcTransaction
	err := rpcClient.CallContext(ctx, &json, "eth_getTransactionByBlockHashAndIndex", blockHash, index)
	if err != nil {
		log.Print("Error fetching block info:", err)
	}
	return c.JSON(StringifyTransaction(json))
}

// getTransactionReceiptByHash retrieves transaction receipt by hash and returns it as JSON.
func getTransactionReceiptByHash(c *fiber.Ctx) error {
	hash := c.Params("hash")
	// Check if hash is a valid transaction hash
	if hashRegex.MatchString(hash) {
		var ctx = context.Background()
		var transactionReceipt txReceipt
		err := rpcClient.CallContext(ctx, &transactionReceipt, "eth_getTransactionReceipt", hash)
		if err != nil {
			log.Print("Error fetching transaction info:", err)
		}
		return c.JSON(StringifyReceipt(transactionReceipt))
	} else {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid transaction hash",
		})
	}
}

func getUncleByBlockIdentifierAndIndex(c *fiber.Ctx) error {
	identifier := c.Params("identifier")
	index := c.Params("index")
	log.Print(identifier)
	log.Print(index)
	if blockNumberRegex.MatchString(index) {
		// Check if index is a valid number by converting hex to decimal
		_, success := new(big.Int).SetString(index[2:], 16)
		if !success {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid index",
			})
		}
	} else if decimalNumberRegex.MatchString(index) {
		// index is supposed to be a hex string
		index = decimalToHex(index)
		if index == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid index " + index,
			})
		}
	} else {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid index",
		})
	}

	// Check if identifier is a block hash or block number
	if hashRegex.MatchString(identifier) {
		log.Println("Block hash")
		uncle := getUncleByBlockHashAndIndex(c, identifier, index)
		return uncle
	} else if blockNumberRegex.MatchString(identifier) || defaultBlockParamRegex.MatchString(identifier) || decimalNumberRegex.MatchString(identifier) {
		log.Println("Block number")
		uncle := getUncleByBlockNumberAndIndex(c, identifier, index)
		return uncle
	} else {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid identifier",
		})
	}
}

func getUncleByBlockNumberAndIndex(c *fiber.Ctx, numberOrDefaultParameters string, index string) error {
	if decimalNumberRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Decimal number")
		uncle := getUncleByDecimalNumberAndIndex(c, numberOrDefaultParameters, index)
		return uncle
	} else {
		number := numberOrDefaultParameters
		log.Println(number)

		if !defaultBlockParamRegex.MatchString(number) {
			blockNumber, success := new(big.Int).SetString(number[2:], 16)
			if !success {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "Invalid number",
				})
			}
			log.Println(blockNumber)
		}

		var ctx = context.Background()
		var uncle *types.Header

		err := rpcClient.CallContext(ctx, &uncle, "eth_getUncleByBlockNumberAndIndex", number, index)
		if err != nil {
			log.Print("Error fetching uncle:", err)
		}
		return c.JSON(StringifyHeader(uncle))
	}
}

func getUncleByDecimalNumberAndIndex(c *fiber.Ctx, number string, index string) error {
	hexNumber := decimalToHex(number)
	if hexNumber == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid number " + number,
		})
	}

	var ctx = context.Background()
	var uncle *types.Header

	err := rpcClient.CallContext(ctx, &uncle, "eth_getUncleByBlockNumberAndIndex", hexNumber, index)
	if err != nil {
		log.Print("Error fetching uncle:", err)
	}
	return c.JSON(StringifyHeader(uncle))
}

func getUncleByBlockHashAndIndex(c *fiber.Ctx, hash string, index string) error {
	var ctx = context.Background()
	var uncle *types.Header

	err := rpcClient.CallContext(ctx, &uncle, "eth_getUncleByBlockHashAndIndex", hash, index)
	if err != nil {
		log.Print("Error fetching uncle:", err)
	}
	return c.JSON(StringifyHeader(uncle))
}

func getUncleCountByBlockIdentifier(c *fiber.Ctx) error {
	identifier := c.Params("identifier")
	log.Print(identifier)

	// Check if identifier is a block hash or block number
	if hashRegex.MatchString(identifier) {
		log.Println("Block hash")
		uncleCount := getUncleCountByBlockHash(c, identifier)
		return uncleCount
	} else if blockNumberRegex.MatchString(identifier) || defaultBlockParamRegex.MatchString(identifier) || decimalNumberRegex.MatchString(identifier) {
		log.Println("Block number")
		uncleCount := getUncleCountByBlockNumber(c, identifier)
		return uncleCount
	} else {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid identifier",
		})
	}
}

func getUncleCountByBlockHash(c *fiber.Ctx, hash string) error {
	var ctx = context.Background()
	var uncleCount string

	err := rpcClient.CallContext(ctx, &uncleCount, "eth_getUncleCountByBlockHash", hash)
	if err != nil {
		log.Print("Error fetching uncle count:", err)
	}
	return c.JSON(StringifyCount(uncleCount))
}

func getUncleCountByBlockNumber(c *fiber.Ctx, numberOrDefaultParameters string) error {
	if decimalNumberRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Decimal number")
		uncleCount := getUncleCountByDecimalNumber(c, numberOrDefaultParameters)
		return uncleCount
	} else {
		number := numberOrDefaultParameters
		log.Println(number)

		if !defaultBlockParamRegex.MatchString(number) {
			blockNumber, success := new(big.Int).SetString(number[2:], 16)
			if !success {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "Invalid number",
				})
			}
			log.Println(blockNumber)
		}

		var ctx = context.Background()
		var uncleCount string

		err := rpcClient.CallContext(ctx, &uncleCount, "eth_getUncleCountByBlockNumber", number)
		if err != nil {
			log.Print("Error fetching uncle count:", err)
		}
		return c.JSON(StringifyCount(uncleCount))
	}
}

func getUncleCountByDecimalNumber(c *fiber.Ctx, number string) error {
	hexNumber := decimalToHex(number)
	if hexNumber == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid number " + number,
		})
	}

	var ctx = context.Background()
	var uncleCount string

	err := rpcClient.CallContext(ctx, &uncleCount, "eth_getUncleCountByBlockNumber", hexNumber)
	if err != nil {
		log.Print("Error fetching uncle count:", err)
	}
	return c.JSON(StringifyCount(uncleCount))
}

// getBlockTransactionCountByIdentifier retrieves block transaction count by block hash or block number and returns it as JSON.
func getBlockTransactionCountByIdentifier(c *fiber.Ctx) error {
	identifier := c.Params("identifier")
	log.Print(identifier)

	// Check if identifier is a block hash or block number
	if hashRegex.MatchString(identifier) {
		log.Println("Block hash")
		blockTransactionCount := getBlockTransactionCountByBlockHash(c, identifier)
		return blockTransactionCount
	} else if blockNumberRegex.MatchString(identifier) || defaultBlockParamRegex.MatchString(identifier) || decimalNumberRegex.MatchString(identifier) {
		log.Println("Block number")
		blockTransactionCount := getBlockTransactionCountByBlockNumber(c, identifier)
		return blockTransactionCount
	} else {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid identifier",
		})
	}
}

// getBlockTransactionCountByBlockHash retrieves block transaction count by block hash and returns it as JSON.
func getBlockTransactionCountByBlockHash(c *fiber.Ctx, hash string) error {
	var ctx = context.Background()
	var blockTransactionCount string

	err := rpcClient.CallContext(ctx, &blockTransactionCount, "eth_getBlockTransactionCountByHash", hash)
	if err != nil {
		log.Print("Error fetching block transaction count:", err)
	}

	return c.JSON(StringifyCount(blockTransactionCount))
}

// getBlockTransactionCountByBlockNumber retrieves block transaction count by block number and returns it as JSON.
func getBlockTransactionCountByBlockNumber(c *fiber.Ctx, numberOrDefaultParameters string) error {
	if decimalNumberRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Decimal number")
		blockTransactionCount := getBlockTransactionCountByDecimalNumber(c, numberOrDefaultParameters)
		return blockTransactionCount
	} else {
		number := numberOrDefaultParameters
		log.Println(number)

		if !defaultBlockParamRegex.MatchString(number) {
			blockNumber, success := new(big.Int).SetString(number[2:], 16)
			if !success {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "Invalid number",
				})
			}
			log.Println(blockNumber)
		}

		var ctx = context.Background()
		var blockTransactionCount string

		err := rpcClient.CallContext(ctx, &blockTransactionCount, "eth_getBlockTransactionCountByNumber", number)
		if err != nil {
			log.Print("Error fetching block transaction count:", err)
		}

		return c.JSON(StringifyCount(blockTransactionCount))
	}
}

func getBlockTransactionCountByDecimalNumber(c *fiber.Ctx, number string) error {
	hexNumber := decimalToHex(number)
	if hexNumber == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid number " + number,
		})
	}

	var ctx = context.Background()
	var blockTransactionCount string

	err := rpcClient.CallContext(ctx, &blockTransactionCount, "eth_getBlockTransactionCountByNumber", hexNumber)
	if err != nil {
		log.Print("Error fetching block transaction count:", err)
	}
	return c.JSON(StringifyCount(blockTransactionCount))
}

// getBlockNumber retrieves block number and returns it as JSON.
func getBlockNumber(c *fiber.Ctx) error {
	var ctx = context.Background()
	var blockNumber string

	err := rpcClient.CallContext(ctx, &blockNumber, "eth_blockNumber")
	if err != nil {
		log.Print("Error fetching block number:", err)
	}
	return c.JSON(StringifyCount(blockNumber))
}

// sendRawTransaction sends a signed transaction to the network and returns the transaction hash as JSON.
func sendRawTransaction(c *fiber.Ctx) error {
	data := c.Params("data")
	log.Print(data)

	var transactionHash common.Hash
	var ctx = context.Background()
	err := rpcClient.CallContext(ctx, &transactionHash, "eth_sendRawTransaction", data)
	if err != nil {
		log.Print("Error sending transaction:", err)
	}
	return c.JSON(transactionHash)
}

// sendTransaction sends a transaction to the network, and signs it using the account specified in `from` and returns the transaction hash as JSON.
func sendTransaction(c *fiber.Ctx) error {

	// get the request body
	obj := new(ModifiedRequestBody)

	if err := c.BodyParser(obj); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// Check if from is a valid address
	if !common.IsHexAddress(obj.From.String()) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid from address",
		})
	}

	// Check if it is a contract creation transaction
	if obj.To != nil {
		var encodingErr error
		obj.Input, encodingErr = EncodeFunctionSignature(obj.Input)
		if encodingErr != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": encodingErr.Error(),
			})
		}
		// Check if to is a valid address
		if !common.IsHexAddress(obj.To.String()) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid to address",
			})
		}
	}

	// send only non empty values in the request body from obj
	objMap := make(map[string]interface{})
	objMap["from"] = obj.From
	// check if to isn't empty
	if obj.To != nil {
		objMap["to"] = obj.To
	}
	// check if gas isn't empty
	if obj.Gas != "" {
		objMap["gas"] = obj.Gas
	}
	// check if gasPrice isn't empty
	if obj.GasPrice != "" {
		objMap["gasPrice"] = obj.GasPrice
	}
	// check if value isn't empty
	if obj.Value != "" {
		objMap["value"] = obj.Value
	}
	// check if nonce isn't empty
	if obj.Nonce != "" {
		objMap["nonce"] = obj.Nonce
	}
	objMap["data"] = obj.Input
	print(obj.Input)
	// convert gas, gasPrice, value and nonce to hex from decimal
	// gas = decimalToHex(gas)
	// gasPrice = decimalToHex(gasPrice)
	// value = decimalToHex(value)
	// nonce = decimalToHex(nonce)

	// reconstruct the request body
	// obj.Gas = gas
	// obj.GasPrice = gasPrice
	// obj.Value = value
	// obj.Nonce = nonce

	var ctx = context.Background()
	var transactionHash common.Hash
	err := rpcClient.CallContext(ctx, &transactionHash, "eth_sendTransaction", objMap)
	if err != nil {
		log.Print("Error sending transaction:", err)
	}
	return c.JSON(transactionHash)
}

// getBalanceOfAddressAtBlock retrieves balance of address at block number and returns it in Wei.
func getBalanceOfAddressAtBlock(c *fiber.Ctx) error {
	address := c.Params("address")
	numberOrDefaultParameters := c.Params("identifier", "latest")
	log.Print(address)
	log.Print(numberOrDefaultParameters)

	// Check if address is a valid address
	if !common.IsHexAddress(address) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid address",
		})
	}

	// Check if identifier is a block number
	if blockNumberRegex.MatchString(numberOrDefaultParameters) || defaultBlockParamRegex.MatchString(numberOrDefaultParameters) || decimalNumberRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Block number")
		balance := getBalanceOfAddressAtBlockNumber(c, address, numberOrDefaultParameters)
		return balance
	} else {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid Block Number",
		})
	}
}

func getBalanceOfAddressAtBlockNumber(c *fiber.Ctx, address string, numberOrDefaultParameters string) error {
	if decimalNumberRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Decimal number")
		balance := getBalanceOfAddressAtDecimalNumber(c, address, numberOrDefaultParameters)
		return balance
	} else {
		number := numberOrDefaultParameters
		log.Println(number)

		if !defaultBlockParamRegex.MatchString(number) {
			blockNumber, success := new(big.Int).SetString(number[2:], 16)
			if !success {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "Invalid number",
				})
			}
			log.Println(blockNumber)
		}

		var ctx = context.Background()
		var balance string

		err := rpcClient.CallContext(ctx, &balance, "eth_getBalance", address, number)
		if err != nil {
			log.Print("Error fetching balance:", err)
		}

		return c.JSON(StringifyCount(balance))
	}
}

func getBalanceOfAddressAtDecimalNumber(c *fiber.Ctx, address string, number string) error {
	hexNumber := decimalToHex(number)
	if hexNumber == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid number " + number,
		})
	}

	var ctx = context.Background()
	var balance string

	err := rpcClient.CallContext(ctx, &balance, "eth_getBalance", address, hexNumber)
	if err != nil {
		log.Print("Error fetching balance:", err)
	}
	return c.JSON(StringifyCount(balance))
}

// getTransactionCountOfAddressAtBlock retrieves transaction count sent from an address at block number and returns it in number.
func getTransactionCountOfAddressAtBlock(c *fiber.Ctx) error {
	address := c.Params("address")
	numberOrDefaultParameters := c.Params("identifier", "latest")
	log.Print(address)
	log.Print(numberOrDefaultParameters)

	// Check if address is a valid address
	if !common.IsHexAddress(address) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid address",
		})
	}

	// Check if identifier is a block number
	if blockNumberRegex.MatchString(numberOrDefaultParameters) || defaultBlockParamRegex.MatchString(numberOrDefaultParameters) || decimalNumberRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Block number")
		transactionCount := getTransactionCountOfAddressAtBlockNumber(c, address, numberOrDefaultParameters)
		return transactionCount
	} else {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid identifier",
		})
	}
}

func getTransactionCountOfAddressAtBlockNumber(c *fiber.Ctx, address string, numberOrDefaultParameters string) error {
	if decimalNumberRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Decimal number")
		transactionCount := getTransactionCountOfAddressAtDecimalNumber(c, address, numberOrDefaultParameters)
		return transactionCount
	} else {
		number := numberOrDefaultParameters
		log.Println(number)

		if !defaultBlockParamRegex.MatchString(number) {
			blockNumber, success := new(big.Int).SetString(number[2:], 16)
			if !success {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "Invalid number",
				})
			}
			log.Println(blockNumber)
		}

		var ctx = context.Background()
		var transactionCount string

		err := rpcClient.CallContext(ctx, &transactionCount, "eth_getTransactionCount", address, number)
		if err != nil {
			log.Print("Error fetching transaction count:", err)
		}

		return c.JSON(StringifyCount(transactionCount))
	}
}

func getTransactionCountOfAddressAtDecimalNumber(c *fiber.Ctx, address string, number string) error {
	hexNumber := decimalToHex(number)
	if hexNumber == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid number " + number,
		})
	}

	var ctx = context.Background()
	var transactionCount string

	err := rpcClient.CallContext(ctx, &transactionCount, "eth_getTransactionCount", address, hexNumber)
	if err != nil {
		log.Print("Error fetching transaction count:", err)
	}
	return c.JSON(StringifyCount(transactionCount))
}

func getCodeOfAddressAtBlock(c *fiber.Ctx) error {
	address := c.Params("address")
	numberOrDefaultParameters := c.Params("identifier", "latest")
	log.Print(address)
	log.Print(numberOrDefaultParameters)

	// Check if address is a valid address
	if !common.IsHexAddress(address) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid address",
		})
	}

	// Check if identifier is a block number
	if blockNumberRegex.MatchString(numberOrDefaultParameters) || defaultBlockParamRegex.MatchString(numberOrDefaultParameters) || decimalNumberRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Block number")
		code := getCodeOfAddressAtBlockNumber(c, address, numberOrDefaultParameters)
		return code
	} else {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid identifier",
		})
	}
}

func getCodeOfAddressAtBlockNumber(c *fiber.Ctx, address string, numberOrDefaultParameters string) error {
	if decimalNumberRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Decimal number")
		code := getCodeOfAddressAtDecimalNumber(c, address, numberOrDefaultParameters)
		return code
	} else {
		number := numberOrDefaultParameters
		log.Println(number)

		if !defaultBlockParamRegex.MatchString(number) {
			blockNumber, success := new(big.Int).SetString(number[2:], 16)
			if !success {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "Invalid number",
				})
			}
			log.Println(blockNumber)
		}

		var ctx = context.Background()
		var code string

		err := rpcClient.CallContext(ctx, &code, "eth_getCode", address, number)
		if err != nil {
			log.Print("Error fetching code:", err)
		}
		return c.JSON(code)
	}
}

func getCodeOfAddressAtDecimalNumber(c *fiber.Ctx, address string, number string) error {
	hexNumber := decimalToHex(number)
	if hexNumber == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid number " + number,
		})
	}

	var ctx = context.Background()
	var code string

	err := rpcClient.CallContext(ctx, &code, "eth_getCode", address, hexNumber)
	if err != nil {
		log.Print("Error fetching code:", err)
	}
	return c.JSON(code)
}

func getStorageAtAddressAndPositionAtBlock(c *fiber.Ctx) error {
	address := c.Params("address")
	position := c.Params("position")
	numberOrDefaultParameters := c.Params("identifier", "latest")
	log.Print(address)
	log.Print(position)
	log.Print(numberOrDefaultParameters)

	key := c.Query("map")
	log.Print(key)

	// Check if address is a valid address
	if !common.IsHexAddress(address) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid address",
		})
	}

	// convert position to hex from decimal if it is decimal
	if len(position) >= 2 && position[:2] == "0x" {
		// position is already in hex
	} else {
		position = decimalToHex(position)
		if position == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid position " + position,
			})
		}
	}

	// Check if identifier is a block number
	if blockNumberRegex.MatchString(numberOrDefaultParameters) || defaultBlockParamRegex.MatchString(numberOrDefaultParameters) || decimalNumberRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Block number")
		storage := getStorageAtAddressAndPositionAtBlockNumber(c, address, position, numberOrDefaultParameters, key)
		return storage
	} else {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid identifier",
		})
	}
}

func getStorageAtAddressAndPositionAtBlockNumber(c *fiber.Ctx, address string, position string, numberOrDefaultParameters string, key string) error {
	if decimalNumberRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Decimal number")
		storage := getStorageAtAddressAndPositionAtDecimalNumber(c, address, position, numberOrDefaultParameters, key)
		return storage
	} else {
		number := numberOrDefaultParameters
		log.Println(number)

		if !defaultBlockParamRegex.MatchString(number) {
			blockNumber, success := new(big.Int).SetString(number[2:], 16)
			if !success {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "Invalid number",
				})
			}
			log.Println(blockNumber)
		}

		var ctx = context.Background()
		var storage hexutil.Bytes

		// if key is empty then fetch storage at position
		if key == "" {
			err := rpcClient.CallContext(ctx, &storage, "eth_getStorageAt", address, position, number)
			if err != nil {
				log.Print("Error fetching storage:", err)
			}
			return c.JSON(storage)
		} else {
			final_position_hash := GetMapPosition(key, position)

			err := rpcClient.CallContext(ctx, &storage, "eth_getStorageAt", address, final_position_hash, number)
			if err != nil {
				log.Print("Error fetching storage:", err)
			}
			return c.JSON(storage)
		}
	}
}

func getStorageAtAddressAndPositionAtDecimalNumber(c *fiber.Ctx, address string, position string, number string, key string) error {
	hexNumber := decimalToHex(number)
	if hexNumber == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid number " + number,
		})
	}

	var ctx = context.Background()
	var storage string

	if key == "" {
		err := rpcClient.CallContext(ctx, &storage, "eth_getStorageAt", address, position, hexNumber)
		if err != nil {
			log.Print("Error fetching storage:", err)
		}
		return c.JSON(storage)
	} else {
		final_position_hash := GetMapPosition(key, position)

		err := rpcClient.CallContext(ctx, &storage, "eth_getStorageAt", address, final_position_hash, hexNumber)
		if err != nil {
			log.Print("Error fetching storage:", err)
		}
		return c.JSON(storage)
	}
}

func callContractAtBlock(c *fiber.Ctx) error {
	numberOrDefaultParameters := c.Params("identifier", "latest")
	log.Print(numberOrDefaultParameters)

	if decimalNumberRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Decimal number")
		contractResponse := callContractAtDecimalNumber(c, numberOrDefaultParameters)
		return contractResponse
	} else {
		number := numberOrDefaultParameters
		log.Println(number)

		if !defaultBlockParamRegex.MatchString(number) {
			blockNumber, success := new(big.Int).SetString(number[2:], 16)
			if !success {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "Invalid number",
				})
			}
			log.Println(blockNumber)
		}

		// request body for eth_call
		obj := new(RequestBody)

		// bind request body to obj
		if err := c.BodyParser(obj); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		var encodingError error
		obj.Input, encodingError = EncodeFunctionSignature(obj.Input)
		if encodingError != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": encodingError.Error(),
			})
		}

		// send only non empty values in the request body from obj
		objMap := map[string]interface{}{
			"from": obj.From,
			"to":   obj.To,
		}

		// check if gas is empty
		if obj.Gas != "" {
			objMap["gas"] = obj.Gas
		}
		// check if gasPrice is empty
		if obj.GasPrice != "" {
			objMap["gasPrice"] = obj.GasPrice
		}
		// check if value is empty
		if obj.Value != "" {
			objMap["value"] = obj.Value
		}
		if len(obj.Input) > 0 {
			objMap["data"] = obj.Input
		}
		log.Println(objMap)

		// convert gas, gasPrice, value and nonce to hex from decimal
		// gas = decimalToHex(gas)
		// gasPrice = decimal
		// value = decimalToHex(value)
		// nonce = decimalToHex(nonce)

		// // reconstruct the request body
		// obj.Gas = gas
		// obj.GasPrice = gasPrice
		// obj.Value = value
		// obj.Nonce = nonce

		var ctx = context.Background()
		var result string
		err := rpcClient.CallContext(ctx, &result, "eth_call", objMap, number)
		if err != nil {
			log.Print("Error calling contract:", err.Error())
		}
		return c.JSON(result)
	}
}

func callContractAtDecimalNumber(c *fiber.Ctx, number string) error {
	hexNumber := decimalToHex(number)
	if hexNumber == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid number " + number})
	}

	// get the request body
	obj := new(RequestBody)

	if err := c.BodyParser(obj); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	log.Print(obj)

	// send only non empty values in the request body from obj
	objMap := map[string]interface{}{
		"from": obj.From,
		"to":   obj.To,
	}
	// check if gas is empty
	if obj.Gas != "" {
		objMap["gas"] = obj.Gas
	}
	// check if gasPrice is empty
	if obj.GasPrice != "" {
		objMap["gasPrice"] = obj.GasPrice
	}
	// check if value is empty
	if obj.Value != "" {
		objMap["value"] = obj.Value
	}
	if obj.Input != "" {
		objMap["data"] = obj.Input
	}

	// convert gas, gasPrice, value and nonce to hex from decimal
	// gas = decimalToHex(gas)
	// gasPrice = decimal
	// value = decimalToHex(value)
	// nonce = decimalToHex(nonce)

	// // reconstruct the request body
	// obj.Gas = gas
	// obj.GasPrice = gasPrice
	// obj.Value = value
	// obj.Nonce = nonce

	var ctx = context.Background()
	var result string
	err := rpcClient.CallContext(ctx, &result, "eth_call", objMap, hexNumber)
	if err != nil {
		log.Print("Error calling contract:", err)
	}
	return c.JSON(result)
}
func estimateGas(c *fiber.Ctx) error {
	// get the request body
	obj := new(RequestBody)

	if err := c.BodyParser(obj); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// send only non empty values in the request body from obj
	objMap := map[string]interface{}{
		"from": obj.From,
		"to":   obj.To,
	}
	// check if gas is empty
	if obj.Gas != "" {
		objMap["gas"] = obj.Gas
	}
	// check if gasPrice is empty
	if obj.GasPrice != "" {
		objMap["gasPrice"] = obj.GasPrice
	}
	// check if value is empty
	if obj.Value != "" {
		objMap["value"] = obj.Value
	}
	if obj.Input != "" {
		objMap["data"] = obj.Input
	}

	// convert gas, gasPrice, value and nonce to hex from decimal
	// gas = decimalToHex(gas)
	// gasPrice = decimal
	// value = decimalToHex(value)
	// nonce = decimalToHex(nonce)

	// // reconstruct the request body
	// obj.Gas = gas
	// obj.GasPrice = gasPrice
	// obj.Value = value
	// obj.Nonce = nonce

	var ctx = context.Background()
	var result string
	err := rpcClient.CallContext(ctx, &result, "eth_estimateGas", objMap)
	if err != nil {
		log.Print("Error estimating gas:", err)
	}
	return c.JSON(result)
}

func EncodeFunctionSignature(functionSignatureWithArgs string) (string, error) {
	functionSignature, argTypes, argsString, err := separateSignatureAndArgs(functionSignatureWithArgs)
	if err != nil {
		return "", err
	}
	// Find the position of the first opening parenthesis
	openParenIndex := strings.Index(functionSignature, "(")
	if openParenIndex == -1 {
		return "", errors.New("invalid function signature format,open")
	}

	// Find the position of the last closing parenthesis
	closeParenIndex := strings.LastIndex(functionSignature, ")")
	if closeParenIndex == -1 || closeParenIndex < openParenIndex {
		return "", errors.New("invalid function signature format")
	}

	// Handle the case where there are no arguments
	var args []string
	// var err error
	if argsString == "" {
		args = []string{}
	} else {
		args, err = splitArgs(strings.TrimLeft(argsString, ","))
		if err != nil {
			return "", err
		}
	}

	for i := range argTypes {
		argTypes[i] = normalizeType(strings.TrimSpace(argTypes[i]))
	}

	// Check if the number of arguments matches the function signature
	if len(args) != len(argTypes) {
		return "", errors.New("number of arguments doesn't match function signature")
	}

	// Recombine the normalized function signature
	normalizedFunctionSignature := functionSignature[:openParenIndex+1] + strings.Join(argTypes, ",") + ")"

	// Keccak hash of function signature
	sig := crypto.Keccak256Hash([]byte(normalizedFunctionSignature)).Bytes()[:4]

	// use switch case to determine the type of each argument and encode it accordingly
	var encodedArgs string
	var dynamicData string
	dynamicOffset := len(argTypes) * 32 // Initial offset for dynamic data

	for i := 0; i < len(args); i++ {
		switch argTypes[i] {
		case "int", "int8", "int16", "int24", "int32", "int40", "int48", "int56", "int64", "int72", "int80", "int88", "int96", "int104", "int112", "int120", "int128", "int136", "int144", "int152", "int160", "int168", "int176", "int184", "int192", "int200", "int208", "int216", "int224", "int232", "int240", "int248", "int256", "uint", "uint8", "uint16", "uint24", "uint32", "uint40", "uint48", "uint56", "uint64", "uint72", "uint80", "uint88", "uint96", "uint104", "uint112", "uint120", "uint128", "uint136", "uint144", "uint152", "uint160", "uint168", "uint176", "uint184", "uint192", "uint200", "uint208", "uint216", "uint224", "uint232", "uint240", "uint248", "uint256":
			hexNumber := decimalToHex(args[i])
			if hexNumber == "" {
				return "", errors.New("invalid number passed as argument " + args[i] + ". It should be an integer")
			}
			// prefix hex with zeroes to make it 32 bytes long
			encodedArgs += strings.Repeat("0", 64-len(hexNumber[2:])) + (hexNumber[2:])

		case "address":
			// check if address is a valid address
			if !common.IsHexAddress(args[i]) {
				return "", errors.New("invalid address " + args[i])
			}
			encodedArgs += strings.Repeat("0", 64-len(args[i][2:])) + args[i][2:]

		case "bool":
			if args[i] == "true" {
				encodedArgs += strings.Repeat("0", 63) + "1"
			} else if args[i] == "false" {
				encodedArgs += strings.Repeat("0", 64)
			} else {
				return "", errors.New("invalid boolean value " + args[i])
			}

		case "bytes1", "bytes2", "bytes3", "bytes4", "bytes5", "bytes6", "bytes7", "bytes8", "bytes9", "bytes10", "bytes11", "bytes12", "bytes13", "bytes14", "bytes15", "bytes16", "bytes17", "bytes18", "bytes19", "bytes20", "bytes21", "bytes22", "bytes23", "bytes24", "bytes25", "bytes26", "bytes27", "bytes28", "bytes29", "bytes30", "bytes31", "bytes32":
			// convert args to hex
			// e.g. "abc" to "0x616263"
			hexData := hexutil.Encode([]byte(args[i]))
			// postfix hex with zeroes to make it 32 bytes long
			encodedArgs += hexData[2:] + strings.Repeat("0", 64-len(hexData[2:]))

		// The following are dynamic types.
		// bytes
		// string
		// T[] for any T
		// T[k] for any dynamic T and any k >= 0
		// (T1,...,Tk) if Ti is dynamic for some 1 <= i <= k
		case "string", "bytes":
			dynamicOffsetHex := fmt.Sprintf("%064x", dynamicOffset)
			encodedArgs += dynamicOffsetHex
			// convert string to utf-8 bytes
			utf8Bytes := []byte(args[i])
			// convert utf-8 bytes to hex
			hexData := hexutil.Encode(utf8Bytes)
			// encode length of hexData in hex
			hexLength := fmt.Sprintf("%064x", len(hexData[2:])/2)
			// prefix with zeroes to make it 32 bytes long
			dynamicData += hexLength
			// postfix with zeroes to make it 32 bytes long
			dynamicData += hexData[2:] + strings.Repeat("0", 64-len(hexData[2:]))
			// increment dynamicOffset by length of hexData and length of hexLength
			dynamicOffset += 64 // 32 bytes for length and 32 bytes for data

		case "int[]", "int8[]", "int16[]", "int24[]", "int32[]", "int40[]", "int48[]", "int56[]", "int64[]", "int72[]", "int80[]", "int88[]", "int96[]", "int104[]", "int112[]", "int120[]", "int128[]", "int136[]", "int144[]", "int152[]", "int160[]", "int168[]", "int176[]", "int184[]", "int192[]", "int200[]", "int208[]", "int216[]", "int224[]", "int232[]", "int240[]", "int248[]", "int256[]", "uint[]", "uint8[]", "uint16[]", "uint24[]", "uint32[]", "uint40[]", "uint48[]", "uint56[]", "uint64[]", "uint72[]", "uint80[]", "uint88[]", "uint96[]", "uint104[]", "uint112[]", "uint120[]", "uint128[]", "uint136[]", "uint144[]", "uint152[]", "uint160[]", "uint168[]", "uint176[]", "uint184[]", "uint192[]", "uint200[]", "uint208[]", "uint216[]", "uint224[]", "uint232[]", "uint240[]", "uint248[]", "uint256[]",
			"address[]", "bool[]",
			"bytes1[]", "bytes2[]", "bytes3[]", "bytes4[]", "bytes5[]", "bytes6[]", "bytes7[]", "bytes8[]", "bytes9[]", "bytes10[]", "bytes11[]", "bytes12[]", "bytes13[]", "bytes14[]", "bytes15[]", "bytes16[]", "bytes17[]", "bytes18[]", "bytes19[]", "bytes20[]", "bytes21[]", "bytes22[]", "bytes23[]", "bytes24[]", "bytes25[]", "bytes26[]", "bytes27[]", "bytes28[]", "bytes29[]", "bytes30[]", "bytes31[]", "bytes32[]":
			// Handle any T[] type
			baseType := strings.TrimSuffix(argTypes[i], "[]")
			dynamicOffsetHex := fmt.Sprintf("%064x", dynamicOffset)
			encodedArgs += dynamicOffsetHex

			arrayData := strings.Split(args[i][1:len(args[i])-1], ",")

			// Encode array length
			dynamicData += fmt.Sprintf("%064x", len(arrayData))

			// Encode each element of the array
			for _, elem := range arrayData {
				encodedElem, err := EncodeElement(baseType, elem)
				if err != nil {
					return "", err
				}
				dynamicData += encodedElem
			}

			dynamicOffset += 32 + len(arrayData)*32

		case "bytes[]", "string[]":
			// Handle any T[] type where T itself is also dynamic
			baseType := strings.TrimSuffix(argTypes[i], "[]")
			dynamicOffsetHex := fmt.Sprintf("%064x", dynamicOffset)
			encodedArgs += dynamicOffsetHex

			arrayData := strings.Split(args[i][1:len(args[i])-1], ",")

			// Encode array length
			encodedArgs += fmt.Sprintf("%064x", len(arrayData))

			// Encode offset for the first element
			dynamicOffset = 32 * len(arrayData)
			encodedArgs += fmt.Sprintf("%064x", dynamicOffset)

			// Encode each element of the array
			for i, elem := range arrayData {
				// add offset for each element
				if i < len(arrayData)-1 {
					dynamicOffset += 32 + 32 // 32 bytes for length of string or bytes and 32 bytes for encoded data
					encodedArgs += fmt.Sprintf("%064x", dynamicOffset)
				}
				encodedElem, err := EncodeElement(baseType, elem)
				if err != nil {
					return "", err
				}
				dynamicData += encodedElem
			}

			dynamicOffset += 32 + len(arrayData)*32

		default:
			// Check if it's a fixed-size array of dynamic types
			if strings.HasPrefix(argTypes[i], "string[") || strings.HasPrefix(argTypes[i], "bytes[") {
				// Extract the base type and array size
				baseType := strings.Split(argTypes[i], "[")[0]
				arraySizeStr := strings.TrimSuffix(strings.Split(argTypes[i], "[")[1], "]")
				arraySize, err := strconv.Atoi(arraySizeStr)
				if err != nil {
					return "", fmt.Errorf("invalid array size for type %s", argTypes[i])
				}

				// Parse the array elements
				arrayElements := strings.Split(args[i][1:len(args[i])-1], ",")
				if len(arrayElements) != arraySize {
					return "", fmt.Errorf("array size mismatch for type %s", argTypes[i])
				}

				// Encode offset to the start of the array data
				arrayOffset := fmt.Sprintf("%064x", dynamicOffset)

				var arrayData string
				var arrayElementsOffset string

				// Encode offset of the first element
				elementOffset := 32 * len(arrayElements)
				arrayElementsOffset += fmt.Sprintf("%064x", elementOffset)

				for i, elem := range arrayElements {
					// add offset for each element
					if i < len(arrayData)-1 {
						elementOffset += 32 + 32 // 32 bytes for length of string or bytes and 32 bytes for encoded data
						arrayElementsOffset += fmt.Sprintf("%064x", elementOffset)
					}
					encodedElem, err := EncodeElement(baseType, strings.TrimSpace(elem))
					if err != nil {
						return "", err
					}
					arrayData += encodedElem
				}

				encodedArgs += arrayOffset

				dynamicData += arrayElementsOffset
				dynamicData += arrayData

				dynamicOffset += len(arrayData+arrayElementsOffset) / 2
			} else if strings.HasPrefix(argTypes[i], "(") && strings.HasSuffix(argTypes[i], ")") {
				// Handle tuple types
				// Extract the tuple types
				tupleTypes, err := splitTuple(argTypes[i])
				if err != nil {
					return "", err
				}
				tupleArgs, err := splitTuple(args[i])
				if err != nil {
					return "", err
				}
				if len(tupleTypes) != len(tupleArgs) {
					return "", errors.New("tuple size mismatch")
				}

				// Encode offset to the start of the tuple data
				tupleOffset := fmt.Sprintf("%064x", dynamicOffset)
				encodedArgs += tupleOffset
				var tupleData string
				var elementOffset int
				dynamicCounter := 0

				for i, elem := range tupleArgs {
					if isStaticType(tupleTypes[i]) {
						encodedElem, err := EncodeElement(tupleTypes[i], strings.TrimSpace(elem))
						if err != nil {
							return "", err
						}
						dynamicData += encodedElem
					} else {
						// Encode offset of the first element
						if dynamicCounter == 0 {
							elementOffset = 32 * (len(tupleArgs) - i)
							dynamicData += fmt.Sprintf("%064x", elementOffset)
						} else if i < len(tupleArgs) {
							elementOffset += 32 + 32 // 32 bytes for length of string or bytes and 32 bytes for encoded data
							dynamicData += fmt.Sprintf("%064x", elementOffset)
						}
						encodedElem, err := EncodeElement(tupleTypes[i], strings.TrimSpace(elem))
						if err != nil {
							return "", err
						}
						tupleData += encodedElem
						dynamicCounter++
					}
				}

				dynamicData += tupleData

				dynamicOffset += len(dynamicData) / 2

			} else {
				return "", errors.New("invalid argument type or argument type not supported")
			}
		}
	}

	return hexutil.Encode(sig) + encodedArgs + dynamicData, nil
}

func EncodeElement(elemType, value string) (string, error) {
	switch elemType {
	case "int", "int8", "int16", "int24", "int32", "int40", "int48", "int56", "int64", "int72", "int80", "int88", "int96", "int104", "int112", "int120", "int128", "int136", "int144", "int152", "int160", "int168", "int176", "int184", "int192", "int200", "int208", "int216", "int224", "int232", "int240", "int248", "int256", "uint", "uint8", "uint16", "uint24", "uint32", "uint40", "uint48", "uint56", "uint64", "uint72", "uint80", "uint88", "uint96", "uint104", "uint112", "uint120", "uint128", "uint136", "uint144", "uint152", "uint160", "uint168", "uint176", "uint184", "uint192", "uint200", "uint208", "uint216", "uint224", "uint232", "uint240", "uint248", "uint256":
		hexNumber := decimalToHex(value)
		if hexNumber == "" {
			return "", fmt.Errorf("invalid number: %s", value)
		}
		return fmt.Sprintf("%064s", hexNumber[2:]), nil
	case "address":
		if !common.IsHexAddress(value) {
			return "", fmt.Errorf("invalid address: %s", value)
		}
		return fmt.Sprintf("%064s", value[2:]), nil
	case "bool":
		if value == "true" {
			return strings.Repeat("0", 63) + "1", nil
		} else if value == "false" {
			return strings.Repeat("0", 64), nil
		}
		return "", fmt.Errorf("invalid boolean value: %s", value)
	case "bytes1", "bytes2", "bytes3", "bytes4", "bytes5", "bytes6", "bytes7", "bytes8", "bytes9", "bytes10", "bytes11", "bytes12", "bytes13", "bytes14", "bytes15", "bytes16", "bytes17", "bytes18", "bytes19", "bytes20", "bytes21", "bytes22", "bytes23", "bytes24", "bytes25", "bytes26", "bytes27", "bytes28", "bytes29", "bytes30", "bytes31", "bytes32":
		hexData := hexutil.Encode([]byte(value))
		return hexData[2:] + strings.Repeat("0", 64-len(hexData[2:])), nil
	case "string", "bytes":
		utf8Bytes := []byte(value)
		hexData := hexutil.Encode(utf8Bytes)
		hexLength := fmt.Sprintf("%064x", len(hexData[2:])/2)
		return hexLength + hexData[2:] + strings.Repeat("0", 64-len(hexData[2:])), nil
	case "string[]", "bytes[]":
		args := strings.Split(value[1:len(value)-1], ",")
		// Encode array length
		arrayLengthHex := fmt.Sprintf("%064x", len(args))

		// Encode offset for first element
		offset := 32 * len(args)
		offsetHex := fmt.Sprintf("%064x", offset)

		// Encode each element of the array
		var encodedArgs string
		for i, arg := range args {
			arg = strings.TrimSpace(arg)
			encodedElem, err := EncodeElement(elemType[:len(elemType)-2], arg)
			if err != nil {
				return "", err
			}
			encodedArgs += encodedElem
			// add offset for each element
			if i < len(args)-1 {
				offset += 32 + 32 // 32 bytes for length of string or bytes and 32 bytes for encoded data
				offsetHex += fmt.Sprintf("%064x", offset)
			}
		}

		return arrayLengthHex + offsetHex + encodedArgs, nil

	default:
		return "", fmt.Errorf("unsupported element type: %s", elemType)
	}
}

func separateSignatureAndArgs(input string) (string, []string, string, error) {
	// Find the position of the first opening parenthesis
	firstOpenParen := strings.Index(input, "(")
	if firstOpenParen == -1 {
		return "", nil, "", errors.New("invalid input: missing opening parenthesis")
	}

	// Initialize counters for nested parentheses
	openCount := 0
	closeCount := 0

	// Find the end of the function signature
	signatureEnd := -1
	for i := firstOpenParen; i < len(input); i++ {
		if input[i] == '(' {
			openCount++
		} else if input[i] == ')' {
			closeCount++
		}

		if openCount == closeCount {
			signatureEnd = i
			break
		}
	}

	if signatureEnd == -1 {
		return "", nil, "", errors.New("invalid input: unmatched parentheses in function signature")
	}

	// Extract function signature and arguments
	functionSignature := strings.TrimSpace(input[:signatureEnd+1])
	args := strings.TrimSpace(input[signatureEnd+1:])

	// Remove leading comma from args if present
	args = strings.TrimPrefix(args, ",")

	// Extract argument types
	argTypes, err := extractArgTypes(functionSignature)
	if err != nil {
		return "", nil, "", err
	}

	return functionSignature, argTypes, args, nil
}

func extractArgTypes(functionSignature string) ([]string, error) {
	// Find the position of the first opening parenthesis
	openParenIndex := strings.Index(functionSignature, "(")
	if openParenIndex == -1 {
		return nil, errors.New("invalid function signature: missing opening parenthesis")
	}

	// Find the position of the last closing parenthesis
	closeParenIndex := strings.LastIndex(functionSignature, ")")
	if closeParenIndex == -1 || closeParenIndex < openParenIndex {
		return nil, errors.New("invalid function signature: missing or misplaced closing parenthesis")
	}

	// Extract the argument types string
	argTypesString := functionSignature[openParenIndex+1 : closeParenIndex]

	// Split the argument types
	var argTypes []string
	var currentType string
	parenCount := 0

	for _, char := range argTypesString {
		switch char {
		case '(':
			parenCount++
			currentType += string(char)
		case ')':
			parenCount--
			currentType += string(char)
			if parenCount == 0 && len(currentType) > 0 {
				argTypes = append(argTypes, strings.TrimSpace(currentType))
				currentType = ""
			}
		case ',':
			if parenCount == 0 {
				if len(currentType) > 0 {
					argTypes = append(argTypes, strings.TrimSpace(currentType))
					currentType = ""
				}
			} else {
				currentType += string(char)
			}
		default:
			currentType += string(char)
		}
	}

	if len(currentType) > 0 {
		argTypes = append(argTypes, strings.TrimSpace(currentType))
	}

	return argTypes, nil
}

func isStaticType(typ string) bool {
	// Remove any array brackets and check if it's a fixed-size array
	if strings.Contains(typ, "[") {
		parts := strings.Split(typ, "[")
		if len(parts) != 2 {
			return false // Invalid type format
		}
		if parts[1] == "]" {
			return false // Dynamic-sized array
		}
		// Check if the base type is static
		return isStaticType(parts[0])
	}

	// List of static types
	staticTypes := []string{
		"int", "int8", "int16", "int24", "int32", "int40", "int48", "int56", "int64", "int72", "int80", "int88", "int96", "int104", "int112", "int120", "int128", "int136", "int144", "int152", "int160", "int168", "int176", "int184", "int192", "int200", "int208", "int216", "int224", "int232", "int240", "int248", "int256",
		"uint", "uint8", "uint16", "uint24", "uint32", "uint40", "uint48", "uint56", "uint64", "uint72", "uint80", "uint88", "uint96", "uint104", "uint112", "uint120", "uint128", "uint136", "uint144", "uint152", "uint160", "uint168", "uint176", "uint184", "uint192", "uint200", "uint208", "uint216", "uint224", "uint232", "uint240", "uint248", "uint256",
		"bool",
		"address",
		"bytes1", "bytes2", "bytes3", "bytes4", "bytes5", "bytes6", "bytes7", "bytes8",
		"bytes9", "bytes10", "bytes11", "bytes12", "bytes13", "bytes14", "bytes15", "bytes16",
		"bytes17", "bytes18", "bytes19", "bytes20", "bytes21", "bytes22", "bytes23", "bytes24",
		"bytes25", "bytes26", "bytes27", "bytes28", "bytes29", "bytes30", "bytes31", "bytes32",
	}

	// Check if the type is in the list of static types
	for _, staticType := range staticTypes {
		if typ == staticType {
			return true
		}
	}

	// Check if it's a tuple type
	if strings.HasPrefix(typ, "(") && strings.HasSuffix(typ, ")") {
		// Remove parentheses and split into component types
		innerTypes := strings.Split(typ[1:len(typ)-1], ",")
		// Check if all component types are static
		for _, innerType := range innerTypes {
			if !isStaticType(strings.TrimSpace(innerType)) {
				return false
			}
		}
		return true
	}

	// If we've reached here, it's not a static type
	return false
}

func normalizeType(t string) string {
	// Handle arrays
	if strings.HasSuffix(t, "[]") {
		return normalizeType(strings.TrimSuffix(t, "[]")) + "[]"
	}

	// Handle tuples
	if strings.HasPrefix(t, "(") && strings.HasSuffix(t, ")") {
		inner := t[1 : len(t)-1]
		types := strings.Split(inner, ",")
		for i, typ := range types {
			types[i] = normalizeType(strings.TrimSpace(typ))
		}
		return "(" + strings.Join(types, ",") + ")"
	}

	// Handle basic types
	switch t {
	case "uint":
		return "uint256"
	case "int":
		return "int256"
	}

	// Return the type as-is for other cases
	return t
}

func splitArgs(argsString string) ([]string, error) {
	var args []string
	var current string
	var squareDepth, parenDepth int

	for _, char := range argsString {
		switch char {
		case '[':
			squareDepth++
			current += string(char)
		case ']':
			squareDepth--
			current += string(char)
			if squareDepth < 0 {
				return nil, errors.New("mismatched square brackets in arguments")
			}
		case '(':
			parenDepth++
			current += string(char)
		case ')':
			parenDepth--
			current += string(char)
			if parenDepth < 0 {
				return nil, errors.New("mismatched parentheses in arguments")
			}
		case ',':
			if squareDepth == 0 && parenDepth == 0 {
				args = append(args, strings.TrimSpace(current))
				current = ""
			} else {
				current += string(char)
			}
		default:
			current += string(char)
		}
	}

	if squareDepth != 0 {
		return nil, errors.New("mismatched square brackets in arguments")
	}

	if parenDepth != 0 {
		return nil, errors.New("mismatched parentheses in arguments")
	}

	if current != "" {
		args = append(args, strings.TrimSpace(current))
	}

	return args, nil
}

func splitTuple(tupleString string) ([]string, error) {
	// Remove outer parentheses
	tupleString = strings.TrimSpace(tupleString)
	if !strings.HasPrefix(tupleString, "(") || !strings.HasSuffix(tupleString, ")") {
		return nil, fmt.Errorf("invalid tuple string: %s", tupleString)
	}
	tupleString = tupleString[1 : len(tupleString)-1]

	var args []string
	var current string
	var squareDepth, parenDepth int

	for _, char := range tupleString {
		switch char {
		case '[':
			squareDepth++
			current += string(char)
		case ']':
			squareDepth--
			current += string(char)
			if squareDepth < 0 {
				return nil, fmt.Errorf("mismatched square brackets in tuple: %s", tupleString)
			}
		case '(':
			parenDepth++
			current += string(char)
		case ')':
			parenDepth--
			current += string(char)
			if parenDepth < 0 {
				return nil, fmt.Errorf("mismatched parentheses in tuple: %s", tupleString)
			}
		case ',':
			if squareDepth == 0 && parenDepth == 0 {
				args = append(args, strings.TrimSpace(current))
				current = ""
			} else {
				current += string(char)
			}
		default:
			current += string(char)
		}
	}

	if squareDepth != 0 {
		return nil, fmt.Errorf("mismatched square brackets in tuple: %s", tupleString)
	}

	if parenDepth != 0 {
		return nil, fmt.Errorf("mismatched parentheses in tuple: %s", tupleString)
	}

	if current != "" {
		args = append(args, strings.TrimSpace(current))
	}

	return args, nil
}

func GetMapPosition(key string, position string) string {
	if key[:2] == "0x" {
		key = key[2:]
	}

	key = strings.Repeat("0", 64-len(key)) + key
	// left pad position with 0s to make it 32 bytes long
	position = strings.Repeat("0", 64-len(position[2:])) + position[2:]
	// concatenate key and position
	final_position_hex := key + position
	log.Print(final_position_hex)
	// keccak256 hash of hex encoded final_position
	final_position, _ := hex.DecodeString(final_position_hex)
	final_position_hash := crypto.Keccak256Hash([]byte(final_position))
	log.Print(final_position_hash)
	return final_position_hash.String()
}

func decimalToHex(number string) string {
	// decimal to hexadecimal conversion
	intNumber, success := new(big.Int).SetString(number, 10)
	if !success {
		return ""
	}

	// Check if the number is negative
	if intNumber.Sign() < 0 {
		// For negative numbers, use the two's complement representation
		intNumber.Add(intNumber, new(big.Int).Lsh(big.NewInt(1), 256)) // Add 2^256 to get the two's complement
	}

	hexNumber := fmt.Sprintf("0x%x", intNumber)
	log.Println(hexNumber)
	return hexNumber
}

// StringifyHeader converts given block header and returns stringified values as map
func StringifyHeader(blockInfo *types.Header) map[string]interface{} {
	// return a map of all items in struct
	// TODO: It should be more modular where for each item in a struct it is converted to string using the methods suitable according to the types of item
	stringfied := map[string]interface{}{
		"number":           blockInfo.Number.String(),
		"timestamp":        fmt.Sprint(blockInfo.Time),
		"miner":            blockInfo.Coinbase.String(),
		"difficulty":       blockInfo.Difficulty.String(),
		"size":             blockInfo.Size(),
		"gasUsed":          fmt.Sprint(blockInfo.GasUsed),
		"gasLimit":         fmt.Sprint(blockInfo.GasLimit),
		"extraData":        string(blockInfo.Extra),
		"baseFeePerGas":    blockInfo.BaseFee,
		"hash":             blockInfo.Hash().String(),
		"parentHash":       blockInfo.ParentHash.String(),
		"sha3Uncles":       blockInfo.UncleHash.String(),
		"stateRoot":        blockInfo.Root.String(),
		"nonce":            blockInfo.Nonce,
		"blobGasUsed":      blockInfo.BlobGasUsed,
		"logsBloom":        blockInfo.Bloom,
		"mixHash":          blockInfo.MixDigest.String(),
		"receiptsRoot":     blockInfo.ReceiptHash.String(),
		"transactionsRoot": blockInfo.TxHash.String(),
		"withdrawalsRoot":  blockInfo.WithdrawalsHash,
		"excessBlobGas":    blockInfo.ExcessBlobGas,
	}
	return stringfied
}

// StringifyCount converts given hex string and returns decimal number or nil
func StringifyCount(blockTransactionCount string) *big.Int {
	// converting hex string to decimal
	blockTransactionCount = blockTransactionCount[2:]
	finalTransactionCount, success := new(big.Int).SetString(blockTransactionCount, 16)
	if !success {
		return nil
	}
	return finalTransactionCount
}

// StringifyTransaction converts given transaction and returns stringified values as map
func StringifyTransaction(transaction *rpcTransaction) map[string]interface{} {
	blockNumberDecimal, _ := new(big.Int).SetString(transaction.BlockNumber[2:], 16)
	gasDecimal, _ := new(big.Int).SetString(transaction.Gas[2:], 16)
	gasPriceDecimal, _ := new(big.Int).SetString(transaction.GasPrice[2:], 16)
	nonceDecimal, _ := new(big.Int).SetString(transaction.Nonce[2:], 16)
	transactionIndexDecimal, _ := new(big.Int).SetString(transaction.TransactionIndex[2:], 16)
	valueDecimal, _ := new(big.Int).SetString(transaction.Value[2:], 16)
	vDecimal, _ := new(big.Int).SetString(transaction.V[2:], 16)

	// return a map of all items in struct
	stringified := map[string]interface{}{
		"blockHash":        transaction.BlockHash.String(),
		"blockNumber":      blockNumberDecimal,
		"from":             transaction.From,
		"gas":              gasDecimal,
		"gasPrice":         gasPriceDecimal,
		"hash":             transaction.Hash,
		"input":            transaction.Input,
		"nonce":            nonceDecimal,
		"to":               transaction.To,
		"transactionIndex": transactionIndexDecimal,
		"value":            valueDecimal,
		"v":                vDecimal,
		"r":                transaction.R,
		"s":                transaction.S,
	}

	return stringified
}

// StringifyReceipt converts given transaction receipt and returns stringified values as map
func StringifyReceipt(receipt txReceipt) map[string]interface{} {

	blockNumberDecimal, _ := new(big.Int).SetString(receipt.BlockNumber[2:], 16)
	cumulativeGasDecimal, _ := new(big.Int).SetString(receipt.CumulativeGasUsed[2:], 16)
	gasUsedDecimal, _ := new(big.Int).SetString(receipt.GasUsed[2:], 16)
	effectiveGasDecimal, _ := new(big.Int).SetString(receipt.EffectiveGasPrice[2:], 16)
	transactionIndex, _ := new(big.Int).SetString(receipt.TxIndex[2:], 16)

	// return a map of all items in struct
	stringified := map[string]interface{}{
		"blockHash":         receipt.BlockHash,
		"blockNumber":       blockNumberDecimal,
		"contractAddress":   receipt.ContractAddress,
		"cumulativeGasUsed": cumulativeGasDecimal,
		"effectiveGasPrice": effectiveGasDecimal,
		"gasUsed":           gasUsedDecimal,
		"status":            receipt.Status,
		"transactionHash":   receipt.TxHash,
		"transactionIndex":  transactionIndex,
		"logs":              receipt.Logs,
		"logsBloom":         receipt.Bloom,
		"from":              receipt.From,
		"to":                receipt.To,
		"root":              receipt.Root,
		"type":              receipt.Type,
	}

	return stringified
}
