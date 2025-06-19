# Pump.fun Sniper Bot

This is a Go-based sniper bot for Solana's Pump.fun platform. It monitors the Solana blockchain using Geyser gRPC, detects new token mints on the Pump.fun program, and automatically buys tokens whose market cap exceeds a configurable threshold.
---

## Contact
- [telegram](https://t.me/caterpillardev)
- [twitter](https://x.com/caterpillardev)
  
---

## Features
- **Real-time monitoring** of Pump.fun token mints via Geyser gRPC
- **Automatic buy** of new tokens above a market cap threshold
- **Low-latency**: avoids unnecessary RPC calls, uses pre-fetched data
- **Configurable** via environment variables

---

## Requirements
- Go 1.18+
- Access to a Solana Geyser gRPC endpoint
- Helius or other Solana RPC endpoint (for sending transactions)
- Solana wallet private key (base58 string)

---

## Environment Variables

| Variable                | Description                                                      |
|------------------------|------------------------------------------------------------------|
| `BUYER_PRIVATE_KEY_PATH`| Your Solana wallet private key (base58 string, not a file path)   |
| `GRPC_ENDPOINT`        | Geyser gRPC endpoint URL                                         |
| `GRPC_AUTH_TOKEN`      | Authentication token for the gRPC endpoint                       |
| `SOLANA_RPC_ENDPOINT`  | (Optional) Custom Solana RPC endpoint URL                        |
| `HELIUS_API_KEY`       | (Optional) Helius API key (used if `SOLANA_RPC_ENDPOINT` is unset)|

---

## Setup & Build

1. **Clone the repository**
   ```sh
   git clone <your-repo-url>
   cd pumpfun-sniper-go
   ```

2. **Install dependencies**
   ```sh
   go mod tidy
   ```

3. **Generate gRPC code from proto (if needed)**
   ```sh
   protoc --go_out=. --go-grpc_out=. sniperc/sniperc/proto/geyserpb/geyser.proto
   ```

4. **Build the bot**
   ```sh
   go build -o pumpfun-sniper
   ```

---

## Usage

1. **Set environment variables** (example for PowerShell):
   ```powershell
   $env:BUYER_PRIVATE_KEY_PATH = "<your_base58_private_key>"
   $env:GRPC_ENDPOINT = "<your_geyser_grpc_endpoint>"
   $env:GRPC_AUTH_TOKEN = "<your_grpc_auth_token>"
   $env:HELIUS_API_KEY = "<your_helius_api_key>"  # or set SOLANA_RPC_ENDPOINT directly
   ```

2. **Run the bot**
   ```sh
   ./pumpfun-sniper
   ```

---

## How it Works
- Subscribes to the Pump.fun program on Solana via Geyser gRPC
- Monitors for new token mints
- Calculates the market cap using the initial SOL deposit and current SOL price
- If the market cap is above the threshold (default: $8,000), sends a buy transaction
- Uses a pre-fetched blockhash and SOL price for low-latency execution

---

## Configuration
- **Market cap threshold**: Change `MARKET_CAP_THRESHOLD_USD` in `main.go` to adjust the buy threshold.
- **Buy amount**: Change `BUY_AMOUNT_SOL` in `main.go` to adjust how much SOL to spend per buy.

---

## Security
- **Never share your private key.** Use a dedicated wallet for sniping.
- **Review the code** before running with real funds.

---

## License
MIT 
