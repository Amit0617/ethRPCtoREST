# ethRPCtoREST - RESTful interface for execution layer JSON-RPC APIs

Basically, ethereum nodes have now simpler and intutive requests with human readable responses.

Still clueless? Explainer for where does it fits the equation - https://amit0617.hashnode.dev/web2-to-web3-in-one-shot and slides no. 8 and 9 of the [ppt](https://drive.google.com/file/d/1RoFP3nbU8xhzfa7nP6B5SioDfrzaMao-/view)

## API SPEC [Work in progress, lagging a bit]

`openapi.yaml` consists of OPEN API specification for the REST API.

### How to use it?

1. Download `ethRPCtoREST` binary, make it executable using `chmod +x ./ethRPCtoREST`.
2. Execute it `./ethRPCtoREST $RPC_URL`.
3. And you are done! Open localhost:3000 and visit URL endpoints as mentioned in [API SPEC](#api-spec) which is available at `localhost:3000/docs` endpoint.

### Usage guides

1. [Interact with smart contract functions](https://hackmd.io/@naP4wWiWQdC6Mjk_odET4A/H1JgUkNzC)
2. [Read storage of smart contracts](https://hackmd.io/@naP4wWiWQdC6Mjk_odET4A/B1V3vBWfC)

### How to start contributing?

<a href="https://github.com/Amit0617/ethRPCtoREST/blob/main/CONTRIBUTING.md">CONTRIBUTING.md</a>

This Project was part of Ethereum Protocol Fellowship cohort 4 and proposal can be found [there](https://github.com/eth-protocol-fellows/cohort-four/blob/master/projects/REST-wrapper.md).
