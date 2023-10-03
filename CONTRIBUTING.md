# Contributing guidelines

## Getting Started

In the issues tab, you would find list of tasks as issues. You can get started with issues labelled `good-first-issue`.
Fork it. Clone it. Make changes. Push it. Create a pull request with your changes. Make sure that one PR(Pull request) consists of only one task.

**IMPORTANT**  
If you want to work any particular method comment under the issue with the method name like `eth_call` and not anything else. A new issue will be created for you and assigned to you by me.  
Create a PR and make sure to mention the corresponding issue which will get closed on merging, in the PR Description.

### Low Hanging Fruits

Writing out specifications for the methods could be a good start for anyone. One can see the original spec-compliant response structure from [here](https://ethereum.org/en/developers/docs/apis/json-rpc/) and look at few already done [examples in `openapi.yaml`](https://github.com/Amit0617/ethRPCtoREST/blob/main/openapi.yaml)

### Setting up development enviroment for contributing to application

1. ```bash
   git clone https://github.com/<YOUR_GITHUB_USERNAME>/ethRPCtoREST.git && cd ethRPCtoREST
   ```

2. ```bash
      air -- $RPC_URL
   ```
   Binary for `air` can be downloaded from https://github.com/cosmtrek/air
   And you are done! Open localhost:3000 and visit URL endpoints as mentioned in [API SPEC](#api-spec)
