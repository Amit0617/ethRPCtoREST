import requests
import subprocess
import json

# Your server's base URL
BASE_URL = "http://localhost:3000"

def test_function(function_signature, args):
    # Prepare the request to your server
    url = f"{BASE_URL}/eth/encode"
    # Prepare the payload
    # The payload should contain the function signature and the arguments
    # formatted like given string : "sam(bool,uint256[],bytes), false, [1,2,3], dave"
    if len(args) == 0:
        payload = {
            "data": function_signature
        }
    else:
        payload = {
            "data": f"{function_signature},{','.join(args)}",
        }
    print(payload)
    
    # Make the request to your server
    response = requests.post(url, data=payload)
    server_result = response.json().get('encodedInput', '')
    
    # Run the cast calldata command
    cast_command = ["cast", "calldata", function_signature] + args
    cast_result = subprocess.run(cast_command, capture_output=True, text=True)
    
    # Compare results
    if server_result.lower() == cast_result.stdout.strip().lower():
        print(f"Test passed for {function_signature}")
        print(f"Server result: {server_result}")
        print(f"Cast result: {cast_result.stdout.strip()}")
    else:
        print(f"Test failed for {function_signature}")
        print(f"Server result: {server_result}")
        print(f"Cast result: {cast_result.stdout.strip()}")
    
    print("---")

# Test cases
test_cases = [
    ("transfer(address,uint256)", ["0x742d35Cc6634C0532925a3b844Bc454e4438f44e", "100000000000000000"]),
    ("balanceOf(address)", ["0x742d35Cc6634C0532925a3b844Bc454e4438f44e"]),
    ("approve(address,uint256)", ["0x742d35Cc6634C0532925a3b844Bc454e4438f44e", "1000000000000000000"]),
    ("transferFrom(address,address,uint256)", ["0x742d35Cc6634C0532925a3b844Bc454e4438f44e", "0x5B38Da6a701c568545dCfcB03FcB875f56beddC4", "500000000000000000"]),
    ("name()", []),
    ("symbol()", []),
    ("decimals()", []),
    ("totalSupply()", []),
    ("setString(string)", ["Hello World!"]),
    ("setUint(uint256)", ["123456789"]),
    ("setBool(bool)", ["true"]),
    ("setAddress(address)", ["0x742d35Cc6634C0532925a3b844Bc454e4438f44e"]),
    ("bar(bytes3[2])", ["abc", "def"]),
    ("sam(bytes)", ["dave"]),
    ("sam(bytes,bool,uint256[])", ["dave", "true", "[1,2,3]"]),
    ("setBytes(bytes)", ["1234567890abcdef"]),
    ("setBytes32(bytes32)", ["0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"]),
    ("setIntArray(int256[])", ["[1,2,3,4,5]"]),
    ("setStringArray(string[])", ['["Hello","World"]']),
    ("setTuple(uint256,bool,address)", ["123", "true", "0x742d35Cc6634C0532925a3b844Bc454e4438f44e"]),
]

# Run tests
for signature, args in test_cases:
    test_function(signature, args)