# Pump.fun Sniper Bot — Developer Documentation

## 1. Project Purpose
This bot monitors the Solana blockchain for new token mints on the Pump.fun program using Geyser gRPC, and automatically buys tokens that exceed a configurable market cap threshold. It is designed for low-latency, automated sniping of promising new tokens.

---

## 2. Architecture Overview
- **Geyser gRPC Client:** Subscribes to Solana transaction streams, filtered for the Pump.fun program.
- **Transaction Parser:** Extracts relevant account keys and instruction data from each transaction.
- **Market Cap Calculator:** Uses SOL price and bonding curve math to estimate the new token's market cap.
- **Buy Executor:** If the market cap is above the threshold, builds and sends a buy transaction using a pre-fetched blockhash and cached SOL price.
- **Background Workers:** Periodically update the SOL price and blockhash for low-latency execution.

---

## 3. Main Components
- **main.go**: Entry point, orchestrates all logic.
  - gRPC connection and subscription setup
  - Transaction event loop and parsing
  - Buy transaction construction and submission
- **price_cache.go**: Fetches and caches the SOL price from CoinGecko.
- **sniperc/sniperc/proto/geyserpb/geyser.proto**: Proto definitions for Geyser gRPC (must match the endpoint's schema).

---

## 4. Key Configuration Options
- `MARKET_CAP_THRESHOLD_USD` (in `main.go`): Minimum market cap (USD) to trigger a buy.
- `BUY_AMOUNT_SOL` (in `main.go`): Amount of SOL to spend per buy.
- Environment variables for endpoints, keys, and tokens (see README).

---

## 5. Extending or Modifying the Bot
- **Change buy logic:** Adjust the market cap formula or add new filters in the main event loop.
- **Add logging/alerts:** Insert additional `log.Printf` or integrate with alerting services.
- **Support other programs:** Change the `PUMP_FUN_PROGRAM_ID` and discriminators as needed.
- **Proto changes:** If the Geyser proto changes, update `geyser.proto` and regenerate Go code with `protoc`.

---

## 6. Troubleshooting
- **Build errors about proto:** Ensure your `.proto` file matches the endpoint and is compiled with `protoc`.
- **gRPC connection issues:** Check your `GRPC_ENDPOINT` and `GRPC_AUTH_TOKEN`.
- **No buys happening:** Confirm the bot is receiving transactions and that the market cap logic matches real Pump.fun launches.
- **Latency issues:** Ensure blockhash and SOL price are being updated in the background, and avoid on-demand RPC calls in the buy path.

---

## 7. Proto/gRPC Integration
- The bot relies on the Geyser proto definitions for Solana transaction streaming.
- If the proto changes, update `sniperc/sniperc/proto/geyserpb/geyser.proto` and regenerate Go code:
  ```sh
  protoc --go_out=. --go-grpc_out=. sniperc/sniperc/proto/geyserpb/geyser.proto
  ```
- The proto must define all fields accessed in Go (see main.go for usage).

---

## References
- [Solana Geyser documentation](https://docs.solana.com/developing/clients/jsonrpc-api#geyser-plugins)
- [Helius RPC](https://docs.helius.dev/)
- [Go gRPC documentation](https://grpc.io/docs/languages/go/) 