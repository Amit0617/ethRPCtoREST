package main

import (
	"context"
	// "fmt"
	"math/big"

	"log"

	// "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	// "github.com/ethereum/go-ethereum/ethclient/gethclient"
	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()

	client, err := ethclient.Dial("https://eth-goerli.g.alchemy.com/v2/inGVdrl_rBxo9cs52HpbxtdJmfwSeTet")
	if err != nil {
		log.Fatal(err)
	}

	app.Get("/eth/block/:identifier", func(c *fiber.Ctx) error {
		identifier := c.Params("identifier")
		blockNumber, success := new(big.Int).SetString(identifier, 10)
		if !success {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid identifier",
			})
		}
		blockInfo, err := client.BlockByNumber(context.Background(), blockNumber)
		if err != nil {
			log.Fatal("Error fetching block info:", err)
		}
		return c.JSON(blockInfo)
		// return c.SendString("Hello, World 👋!")
	})

	log.Fatal(app.Listen(":3000"))
}
