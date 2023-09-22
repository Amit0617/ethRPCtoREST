package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"regexp"

	"github.com/joho/godotenv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/gofiber/fiber/v2"
)

var RPC_URL string
var client *ethclient.Client
var rpcClient *rpc.Client

// A Block hash is 32 bytes long and hence 64 characters long plus 0x prefix
var hashRegex = regexp.MustCompile(`^0x[0-9a-f]{64}$`)

// A block number also allows default block identifiers such as "earliest", "latest" and "pending"
// TODO: A block number can also be a decimal number without 0x prefix (part of my proposal)
// A block number can also be a hex number with 0x prefix
// A block number will always consist of a non-zero character after 0x, except for "0x0".

// Regex to allow for default block identifiers
var blockNumberRegex = regexp.MustCompile(`^0x([1-9a-f]+[0-9a-f]*|0)$`)
var defaultBlockParamRegex = regexp.MustCompile(`^(earliest|latest|pending|safe|finalized)$`)

func main() {
	app := fiber.New()
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatalf("Error loading environment variables file")
	}

	RPC_URL = os.Getenv("RPC_URL")
	log.Println(RPC_URL)

	client, err = ethclient.Dial(RPC_URL)
	if err != nil {
		log.Fatal(err)
	}

	rpcClient, err = rpc.Dial(RPC_URL)
	if err != nil {
		log.Fatal(err)
	}

	app.Get("/eth/block/:identifier", getBlockByIdentifier)
	app.Get("/eth/transaction/:hash", getTransactionByHash)
	app.Get("/eth/transaction/block/:identifier/:index", getTransactionByIdentifierAndIndex)
	app.Get("/eth/transaction/receipt/:hash", getTransactionReceiptByHash)
	app.Get("/eth/uncle/block/:identifier/:index", getUncleByBlockIdentifierAndIndex)
	app.Get("/eth/unclecount/block/:identifier", getUncleCountByBlockIdentifier)

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
	} else if blockNumberRegex.MatchString(identifier) || defaultBlockParamRegex.MatchString(identifier) {
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
	log.Println(blockHash)
	block, err := client.HeaderByHash(context.Background(), blockHash)
	if err != nil {
		log.Print("Error fetching block info:", err)
	}
	return c.JSON(block)
}

// getBlockByNumber retrieves block information by block number or default block parameters and returns it as JSON.
func getBlockByNumber(c *fiber.Ctx, numberOrDefaultParameters string, includeTx bool) error {
	if defaultBlockParamRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Default block parameters")
		blockInfo := getBlockByDefaultBlockParameters(c, numberOrDefaultParameters, includeTx)
		return blockInfo
	} else {
		number := numberOrDefaultParameters
		number = number[2:] // Remove 0x prefix
		log.Println(number)

		blockNumber, success := new(big.Int).SetString(number, 16)
		if !success {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid number",
			})
		}
		log.Println(blockNumber)
		blockInfo, err := client.HeaderByNumber(context.Background(), blockNumber)
		if err != nil {
			log.Print("Error fetching block info:", err)
		}
		return c.JSON(blockInfo)
	}
}

func getBlockByDefaultBlockParameters(c *fiber.Ctx, defaultBlockParameters string, includeTx bool) error {
	var ctx = context.Background()
	var blockInfo *types.Header
	// currently we are dealing with header only we will add query params to get full block
	err := rpcClient.CallContext(ctx, &blockInfo, "eth_getBlockByNumber", defaultBlockParameters, includeTx)
	if err != nil {
		log.Print("Error fetching block info:", err)
	}
	return c.JSON(blockInfo)
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
	index := c.Params("index")
	log.Print(identifier)
	log.Print(index)
	// Check if index is a valid uint number by converting hex to uint
	indexNumber, success := new(big.Int).SetString(index[2:], 16)
	if !success {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid index",
		})
	}

	// Check if identifier is a block hash or block number
	if hashRegex.MatchString(identifier) {
		log.Println("Block hash")
		blockInfo := getTransactionByBlockHashAndIndex(c, identifier, uint(indexNumber.Uint64()))
		return blockInfo
	} else if blockNumberRegex.MatchString(identifier) || defaultBlockParamRegex.MatchString(identifier) {
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
	type txExtraInfo struct {
		BlockNumber *string         `json:"blockNumber,omitempty"`
		BlockHash   *common.Hash    `json:"blockHash,omitempty"`
		From        *common.Address `json:"from,omitempty"`
	}
	type rpcTransaction struct {
		tx *types.Transaction
		txExtraInfo
	}
	var json *rpcTransaction

	if defaultBlockParamRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Default block parameters")
		var ctx = context.Background()
		err := rpcClient.CallContext(ctx, &json, "eth_getTransactionByBlockNumberAndIndex", numberOrDefaultParameters, index)
		if err != nil {
			log.Print("Error fetching transaction info:", err)
		}
		return c.JSON(json)
	} else {
		number := numberOrDefaultParameters
		number = number[2:] // Remove 0x prefix
		log.Println(number)

		blockNumber, success := new(big.Int).SetString(number, 16)
		if !success {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid number",
			})
		}
		log.Println(blockNumber)
		var ctx = context.Background()
		err := rpcClient.CallContext(ctx, &json, "eth_getTransactionByBlockNumberAndIndex", toBlockNumArg(blockNumber), index)
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

// redefining function from go-ethereum/ethclient/ethclient.go
func toBlockNumArg(number *big.Int) string {
	if number == nil {
		return "latest"
	}
	if number.Sign() >= 0 {
		return hexutil.EncodeBig(number)
	}
	// It's negative.
	if number.IsInt64() {
		return rpc.BlockNumber(number.Int64()).String()
	}
	// It's negative and large, which is invalid.
	return fmt.Sprintf("<invalid %d>", number)
}

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
	// Check if index is a valid uint number by converting hex to uint
	_, success := new(big.Int).SetString(index[2:], 16)
	if !success {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid index",
		})
	}

	// Check if identifier is a block hash or block number
	if hashRegex.MatchString(identifier) {
		log.Println("Block hash")
		blockInfo := getUncleByBlockHashAndIndex(c, identifier, index)
		return blockInfo
	} else if blockNumberRegex.MatchString(identifier) || defaultBlockParamRegex.MatchString(identifier) {
		log.Println("Block number")
		blockInfo := getUncleByBlockNumberAndIndex(c, identifier, index)
		return blockInfo
	} else {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid identifier",
		})
	}
}

func getUncleByBlockNumberAndIndex(c *fiber.Ctx, numberOrDefaultParameters string, index string) error {
	if defaultBlockParamRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Default block parameters")
		blockInfo := getUncleByDefaultBlockParametersAndIndex(c, numberOrDefaultParameters, index)
		return blockInfo
	} else {
		number := numberOrDefaultParameters
		log.Println(number)

		blockNumber, success := new(big.Int).SetString(number[2:], 16)
		if !success {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid number",
			})
		}
		log.Println(blockNumber)
		var ctx = context.Background()
		var uncle *types.Header
		err := rpcClient.CallContext(ctx, &uncle, "eth_getUncleByBlockNumberAndIndex", number, index)
		if err != nil {
			log.Print("Error fetching block info:", err)
		}
		return c.JSON(uncle)
	}
}

func getUncleByDefaultBlockParametersAndIndex(c *fiber.Ctx, defaultBlockParameters string, index string) error {
	var ctx = context.Background()
	var uncle *types.Header

	err := rpcClient.CallContext(ctx, &uncle, "eth_getUncleByBlockNumberAndIndex", defaultBlockParameters, index)
	if err != nil {
		log.Print("Error fetching block info:", err)
	}
	return c.JSON(uncle)
}

func getUncleByBlockHashAndIndex(c *fiber.Ctx, hash string, index string) error {
	blockHash := common.HexToHash(hash)
	log.Println(blockHash)

	var ctx = context.Background()
	var blockInfo *types.Header

	err := rpcClient.CallContext(ctx, &blockInfo, "eth_getUncleByBlockHashAndIndex", blockHash, index)
	if err != nil {
		log.Print("Error fetching block info:", err)
	}
	return c.JSON(blockInfo)
}

func getUncleCountByBlockIdentifier(c *fiber.Ctx) error {
	identifier := c.Params("identifier")
	log.Print(identifier)

	// Check if identifier is a block hash or block number
	if hashRegex.MatchString(identifier) {
		log.Println("Block hash")
		uncleCount := getUncleCountByBlockHash(c, identifier)
		return uncleCount
	} else if blockNumberRegex.MatchString(identifier) || defaultBlockParamRegex.MatchString(identifier) {
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
	var uncleCount *big.Int

	err := rpcClient.CallContext(ctx, &uncleCount, "eth_getUncleCountByBlockHash", blockHash)
	if err != nil {
		log.Print("Error fetching block info:", err)
	}
	return c.JSON(uncleCount)
}

func getUncleCountByBlockNumber(c *fiber.Ctx, numberOrDefaultParameters string) error {
	if defaultBlockParamRegex.MatchString(numberOrDefaultParameters) {
		log.Println("Default block parameters")
		uncleCount := getUncleCountByDefaultBlockParameters(c, numberOrDefaultParameters)
		return uncleCount
	} else {
		number := numberOrDefaultParameters
		log.Println(number)

		blockNumber, success := new(big.Int).SetString(number[2:], 16)
		if !success {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid number",
			})
		}
		log.Println(blockNumber)

		var ctx = context.Background()
		var uncleCount *big.Int

		err := rpcClient.CallContext(ctx, &uncleCount, "eth_getUncleCountByBlockNumber", number)
		if err != nil {
			log.Print("Error fetching block info:", err)
		}
		return c.JSON(uncleCount)
	}
}

func getUncleCountByDefaultBlockParameters(c *fiber.Ctx, defaultBlockParameters string) error {
	var ctx = context.Background()
	var uncleCount *big.Int

	err := rpcClient.CallContext(ctx, &uncleCount, "eth_getUncleCountByBlockNumber", defaultBlockParameters)
	if err != nil {
		log.Print("Error fetching block info:", err)
	}
	return c.JSON(uncleCount)
}
