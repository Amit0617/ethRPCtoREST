package main

import (
	"context"
	"encoding/hex"
	"errors"

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
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/gofiber/contrib/swagger"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

var RPC_URL string
var client *ethclient.Client
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

	RPC_URL = os.Args[1]
	log.Println(RPC_URL)

	var err error

	client, err = ethclient.Dial(RPC_URL)
	if err != nil {
		log.Fatal(err)
	}

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

	// TODO:(very low priority) also support shortform apis like /e/b/:identifier, /e/t/:hash, /e/t/b/:identifier/:index, /e/t/r/:hash, /e/u/b/:identifier/:index, /e/uc/b/:identifier
	// TODO: I have an idea that is I will make docs of APIs also on the same server. Docs will came up in conditions like:
	// - when user will hit the server on non-existent route.
	// - when user will hit the server with a route matching the routes of APIs but without any query params. like /eth/block or /eth/transaction these will consist of usage of APIs and expected query params for that route specifically.
	// - One thing to keep in mind that it might be an inconvenient thing for the user to get docs on path with wrong query params. So, make sure to check if the request consists of header "Accept: application/json" or not. If it does then return JSON response else return HTML response.
	log.Fatal(app.Listen(":3000"))
}

// Handlers

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
	blockHash := common.HexToHash(hash)
	log.Println(blockHash, includeTx)
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
		transactionHash := common.HexToHash(hash)
		log.Println(transactionHash)
		transaction, isPending, err := client.TransactionByHash(context.Background(), transactionHash)
		if err != nil {
			log.Print("Error fetching transaction info:", err)
		}
		if isPending {
			// Return 202 Accepted status code
			return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
				"info": "Transaction isn't mined yet",
			})
		}
		return c.JSON(transaction)
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

type txExtraInfo struct {
	BlockNumber *string         `json:"blockNumber,omitempty"`
	BlockHash   *common.Hash    `json:"blockHash,omitempty"`
	From        *common.Address `json:"from,omitempty"`
}

type rpcTransaction struct {
	tx *types.Transaction
	txExtraInfo
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
		return c.JSON(json)

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
		return c.JSON(json)
	}
}

func getTransactionByBlockHashAndIndex(c *fiber.Ctx, hash string, index uint) error {
	blockHash := common.HexToHash(hash)
	log.Println(blockHash)
	block, err := client.TransactionInBlock(context.Background(), blockHash, index)
	if err != nil {
		log.Print("Error fetching block info:", err)
	}
	return c.JSON(block)
}

// getTransactionReceiptByHash retrieves transaction receipt by hash and returns it as JSON.
func getTransactionReceiptByHash(c *fiber.Ctx) error {
	hash := c.Params("hash")
	// Check if hash is a valid transaction hash
	if hashRegex.MatchString(hash) {
		transactionHash := common.HexToHash(hash)
		log.Println(transactionHash)
		transactionReceipt, err := client.TransactionReceipt(context.Background(), transactionHash)
		if err != nil {
			log.Print("Error fetching transaction info:", err)
		}
		return c.JSON(transactionReceipt)
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
	blockHash := common.HexToHash(hash)
	log.Println(blockHash)

	var ctx = context.Background()
	var uncle *types.Header

	err := rpcClient.CallContext(ctx, &uncle, "eth_getUncleByBlockHashAndIndex", blockHash, index)
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
	blockHash := common.HexToHash(hash)
	log.Println(blockHash)

	var ctx = context.Background()
	var uncleCount string

	err := rpcClient.CallContext(ctx, &uncleCount, "eth_getUncleCountByBlockHash", blockHash)
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
	blockHash := common.HexToHash(hash)
	log.Println(blockHash)

	var ctx = context.Background()
	var blockTransactionCount string

	err := rpcClient.CallContext(ctx, &blockTransactionCount, "eth_getBlockTransactionCountByHash", blockHash)
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

	log.Print(obj)

	// send only non empty values in the request body from obj
	objMap := make(map[string]interface{})
	objMap["from"] = obj.From
	// check if to is empty
	if obj.To != nil {
		objMap["to"] = obj.To
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
	// check if nonce is empty
	if obj.Nonce != "" {
		objMap["nonce"] = obj.Nonce
	}
	objMap["data"] = obj.Input

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

func EncodeFunctionSignature(functionSignatureWithArgs string) (string, error) {
	// split string at comma
	data := strings.Split(functionSignatureWithArgs, ",")

	// first element is the function signature, and the rest are arguments
	// keccak hash of function signature
	sig := crypto.Keccak256Hash([]byte(strings.TrimSpace(data[0]))).Bytes()[0:4]
	// get the number of arguments from function signature
	// take out substring between parenthesis "( )"
	argumentsTypesString := strings.TrimSpace(data[0][strings.Index(data[0], "(")+1 : strings.Index(data[0], ")")])
	var argumentsTypes []string
	if len(argumentsTypesString) != 0 {
		argumentsTypes = strings.Split(argumentsTypesString, ",")
	}
	log.Println(argumentsTypes, hexutil.Encode(sig))

	// check if number of arguments is equal to the number of arguments in the function signature
	if len(argumentsTypes) != len(data)-1 {
		return "", errors.New("invalid number of arguments")
	}

	// trim whitespace for both data and argumentsTypes
	for i := 0; i < len(data); i++ {
		data[i] = strings.TrimSpace(data[i])
		if i < len(argumentsTypes) {
			argumentsTypes[i] = strings.TrimSpace(argumentsTypes[i])
		}
	}

	// use switch case to determine the type of each argument and encode it accordingly
	var args string

	for i := 1; i < len(data); i++ {
		switch argumentsTypes[i-1] {
		case "int", "int8", "int16", "int24", "int32", "int40", "int48", "int56", "int64", "int72", "int80", "int88", "int96", "int104", "int112", "int120", "int128", "int136", "int144", "int152", "int160", "int168", "int176", "int184", "int192", "int200", "int208", "int216", "int224", "int232", "int240", "int248", "int256", "uint", "uint8", "uint16", "uint24", "uint32", "uint40", "uint48", "uint56", "uint64", "uint72", "uint80", "uint88", "uint96", "uint104", "uint112", "uint120", "uint128", "uint136", "uint144", "uint152", "uint160", "uint168", "uint176", "uint184", "uint192", "uint200", "uint208", "uint216", "uint224", "uint232", "uint240", "uint248", "uint256":
			hexNumber := decimalToHex(data[i])
			if hexNumber == "" {
				return "", errors.New("Invalid number passed as argument " + data[i] + ". It should be an integer")
			}
			// convert argument to hex
			args += strings.Repeat("0", 64-len(hexNumber[2:])) + (hexNumber[2:])

		case "address":
			// check if address is a valid address
			if !common.IsHexAddress(data[i]) {
				return "", errors.New("invalid address " + data[i])
			}
			args += strings.Repeat("0", 64-len(data[i][2:])) + data[i][2:]
		default:
			return "", errors.New("invalid argument type or argument type not supported")
		}
	}

	return hexutil.Encode(sig) + args, nil
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
