package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	computebudget "github.com/gagliardetto/solana-go/programs/compute-budget"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/rpc"

	pb "sniperc/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

var (
	PUMP_FUN_PROGRAM_ID = "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P"
)

const (
	LamportsPerSOL           = 1_000_000_000
	TotalSupply              = 1_000_000_000
	MARKET_CAP_THRESHOLD_USD = 8000.00
	BUY_AMOUNT_SOL           = 0.001
)

var CREATE_DISCRIMINATOR = []byte{0x18, 0x1e, 0xc8, 0x28, 0x05, 0x1c, 0x07, 0x77}
var PUMPFUN_BUY_DISCRIMINATOR = []byte{0x66, 0x06, 0x3d, 0x12, 0x01, 0xda, 0xeb, 0xea}

type tokenAuth struct {
	token string
}

func (t tokenAuth) GetRequestMetadata(ctx context.Context, in ...string) (map[string]string, error) {
	return map[string]string{"x-token": t.token}, nil
}

func (tokenAuth) RequireTransportSecurity() bool {
	return false
}

var processingMutex sync.Mutex

func main() {
	log.Println("🚀 Starting sniper bot monitoring...")

	buyerPrivateKey := os.Getenv("BUYER_PRIVATE_KEY_PATH")
	if buyerPrivateKey == "" {
		log.Fatal("❌ BUYER_PRIVATE_KEY_PATH environment variable not set")
	}

	grpcEndpoint := os.Getenv("GRPC_ENDPOINT")
	if grpcEndpoint == "" {
		log.Fatal("❌ GRPC_ENDPOINT environment variable not set")
	}

	grpcAuthToken := os.Getenv("GRPC_AUTH_TOKEN")
	if grpcAuthToken == "" {
		log.Fatal("❌ GRPC_AUTH_TOKEN environment variable not set")
	}

	pumpFunPK := solana.MustPublicKeyFromBase58(PUMP_FUN_PROGRAM_ID)
	systemProgramPK := solana.SystemProgramID

	feeRecipientPK := solana.MustPublicKeyFromBase58("G5UZAVbAf46s7cKWoyKu8kYTip9DGTpbLZ2qa9Aq69dP")

	buyerAccount := solana.MustPrivateKeyFromBase58(buyerPrivateKey)
	log.Printf("✅ Buyer's Public Key: %s", buyerAccount.PublicKey().String())

	priceCache := NewPriceCache()
	go priceCache.UpdatePricePeriodically()
	time.Sleep(3 * time.Second)

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(tokenAuth{token: grpcAuthToken}),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(1024*1024*1024),
			grpc.MaxCallSendMsgSize(1024*1024*1024),
		),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             5 * time.Second,
			PermitWithoutStream: true,
		}),
	}

	log.Printf("🔌 Connecting to Geyser: %s", grpcEndpoint)
	conn, err := grpc.Dial(grpcEndpoint, opts...)
	if err != nil {
		log.Fatalf("❌ Failed to connect: %v", err)
	}
	defer conn.Close()
	log.Println("✅ gRPC Connection Established.")

	client := pb.NewGeyserClient(conn)
	ctx := context.Background()

	voteFilter := false
	failedFilter := false
	subReq := &pb.SubscribeRequest{
		Transactions: map[string]*pb.SubscribeRequestFilterTransactions{
			"pump_fun_subscription": {
				Vote:           &voteFilter,
				Failed:         &failedFilter,
				AccountInclude: []string{PUMP_FUN_PROGRAM_ID},
			},
		},
		TransactionsStatus: map[string]*pb.SubscribeRequestFilterTransactions{
			"pump_fun_status": {
				Vote:           &voteFilter,
				Failed:         &failedFilter,
				AccountInclude: []string{PUMP_FUN_PROGRAM_ID},
			},
		},
		Commitment: pb.CommitmentLevel_PROCESSED.Enum(),
	}

	stream, err := client.Subscribe(ctx)
	if err != nil {
		log.Fatalf("❌ Failed to create subscription stream: %v", err)
	}

	if err := stream.Send(subReq); err != nil {
		log.Fatalf("❌ Failed to send subscription request: %v", err)
	}
	log.Println("✅ Subscribed. Waiting for 'create' transactions...")
	log.Printf("🎯 Monitoring for tokens with market cap >= $%.2f", MARKET_CAP_THRESHOLD_USD)

	solanaRpcClient := rpc.New(rpc.MainNetBeta_RPC)

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			log.Println("Geyser stream closed.")
			break
		}
		if err != nil {
			log.Fatalf("❌ Error receiving message from stream: %v", err)
		}

		if txUpdate := resp.GetTransaction(); txUpdate != nil {
			tx := txUpdate.GetTransaction()
			message := tx.Transaction.GetMessage()

			staticAccountKeys := message.GetAccountKeys()
			loadedWritableKeys := tx.Meta.GetLoadedWritableAddresses()
			loadedReadonlyKeys := tx.Meta.GetLoadedReadonlyAddresses()

			fullAccountList := make([][]byte, 0, len(staticAccountKeys)+len(loadedWritableKeys)+len(loadedReadonlyKeys))
			fullAccountList = append(fullAccountList, staticAccountKeys...)
			fullAccountList = append(fullAccountList, loadedWritableKeys...)
			fullAccountList = append(fullAccountList, loadedReadonlyKeys...)

			var pumpFunProgramIndex uint32 = 999
			for i, keyBytes := range fullAccountList {
				if bytes.Equal(keyBytes, pumpFunPK.Bytes()) {
					pumpFunProgramIndex = uint32(i)
					break
				}
			}
			if pumpFunProgramIndex == 999 {
				continue
			}

			for _, instruction := range message.GetInstructions() {
				if instruction.ProgramIdIndex == pumpFunProgramIndex {
					if bytes.HasPrefix(instruction.Data, CREATE_DISCRIMINATOR) {
						instructionAccounts := instruction.GetAccounts()
						if len(instructionAccounts) < 8 {
							continue
						}

						var mintKey, bondingCurveKey, associatedBondingCurveKey solana.PublicKey
						var globalKey, eventAuthorityKey, pumpProgramKey solana.PublicKey
						var creatorKey, creatorVaultKey solana.PublicKey

						knownGlobal := solana.MustPublicKeyFromBase58("4wTV1YmiEkRvAtNtsSGPtUrqRYQMe5SKy2uB4Jjaxnjf")
						knownEventAuth := solana.MustPublicKeyFromBase58("Ce6TQqeHC9p8KetsN6JsjHK7UTZk7nasjjnr7XxXp9F1")
						knownSystemProgram := solana.MustPublicKeyFromBase58("11111111111111111111111111111111")
						knownTokenProgram := solana.MustPublicKeyFromBase58("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")
						knownMetadataProgram := solana.MustPublicKeyFromBase58("metaqbxxUerdq28cj1RbAWkYQm3ybzjb6a8bt518x1s")
						knownATAProgram := solana.MustPublicKeyFromBase58("ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL")
						knownComputeBudget := solana.MustPublicKeyFromBase58("ComputeBudget111111111111111111111111111111")
						knownRent := solana.MustPublicKeyFromBase58("SysvarRent111111111111111111111111111111111")

						var unknownAccounts []solana.PublicKey

						for i, accountBytes := range fullAccountList {
							accountPK := solana.PublicKeyFromBytes(accountBytes)

							if accountPK.Equals(knownGlobal) {
								globalKey = accountPK
							} else if accountPK.Equals(knownEventAuth) {
								eventAuthorityKey = accountPK
							} else if accountPK.Equals(knownSystemProgram) {
							} else if accountPK.Equals(knownTokenProgram) {
							} else if accountPK.Equals(pumpFunPK) {
								pumpProgramKey = accountPK
							} else if accountPK.Equals(knownMetadataProgram) {
							} else if accountPK.Equals(knownATAProgram) {
							} else if accountPK.Equals(knownComputeBudget) {
							} else if accountPK.Equals(knownRent) {
							} else {
								unknownAccounts = append(unknownAccounts, accountPK)
							}

							if i == 0 {
								creatorKey = accountPK
							}
						}

						for _, account := range unknownAccounts {
							accountStr := account.String()

							if len(accountStr) >= 4 && accountStr[len(accountStr)-4:] == "pump" {
								mintKey = account
								break
							}
						}

						if !mintKey.IsZero() {
							var remainingAccounts []solana.PublicKey
							for _, account := range unknownAccounts {
								if !account.Equals(mintKey) && !account.Equals(creatorKey) {
									remainingAccounts = append(remainingAccounts, account)
								}
							}

							if len(remainingAccounts) >= 3 {
								bondingCurveKey = remainingAccounts[0]
								associatedBondingCurveKey = remainingAccounts[1]
								if len(fullAccountList) > 7 {
									creatorVaultKey = solana.PublicKeyFromBytes(fullAccountList[7])
								}
							}
						}

						if mintKey.IsZero() && len(instructionAccounts) > 0 {
							mintKey = solana.PublicKeyFromBytes(fullAccountList[instructionAccounts[0]])
						}
						if bondingCurveKey.IsZero() && len(instructionAccounts) > 2 {
							bondingCurveKey = solana.PublicKeyFromBytes(fullAccountList[instructionAccounts[2]])
						}
						if associatedBondingCurveKey.IsZero() && len(instructionAccounts) > 3 {
							associatedBondingCurveKey = solana.PublicKeyFromBytes(fullAccountList[instructionAccounts[3]])
						}

						var initialSolLamports uint64 = 0

						innerInstructions := tx.Meta.GetInnerInstructions()
						for _, inner := range innerInstructions {
							for _, inst := range inner.GetInstructions() {
								progKey := solana.PublicKeyFromBytes(fullAccountList[inst.GetProgramIdIndex()])
								if progKey.Equals(systemProgramPK) {
									if len(inst.GetData()) >= 8 && binary.LittleEndian.Uint32(inst.GetData()[0:4]) == uint32(system.Instruction_Transfer) {
										sourceKey := solana.PublicKeyFromBytes(fullAccountList[inst.GetAccounts()[0]])
										destinationKey := solana.PublicKeyFromBytes(fullAccountList[inst.GetAccounts()[1]])
										lamports := binary.LittleEndian.Uint64(inst.GetData()[4:])
										if destinationKey.Equals(bondingCurveKey) && sourceKey.Equals(creatorKey) {
											if lamports > initialSolLamports {
												initialSolLamports = lamports
											}
										}
									}
								}
							}
						}

						if initialSolLamports > 0 {
							solPriceUSD := priceCache.Get()
							if solPriceUSD > 0 {

								solDepositedInSOL := float64(initialSolLamports) / float64(LamportsPerSOL)

								const INITIAL_VIRTUAL_SOL = 30.0
								const INITIAL_VIRTUAL_TOKENS = 1073000000.0

								k := INITIAL_VIRTUAL_SOL * INITIAL_VIRTUAL_TOKENS
								virtualSolAfter := INITIAL_VIRTUAL_SOL + solDepositedInSOL
								virtualTokensAfter := k / virtualSolAfter

								currentPriceInSol := virtualSolAfter / virtualTokensAfter
								currentPriceUSD := currentPriceInSol * solPriceUSD

								marketCapUSD := currentPriceUSD * TotalSupply

								if marketCapUSD >= MARKET_CAP_THRESHOLD_USD {
									processingMutex.Lock()
									defer processingMutex.Unlock()

									log.Printf("🎯 TARGET ACQUIRED - Market Cap: $%.2f | Mint: %s", marketCapUSD, mintKey.String())
									log.Printf("🚀 Attempting buy transaction...")

									buyerATA, _, err := solana.FindAssociatedTokenAddress(buyerAccount.PublicKey(), mintKey)
									if err != nil {
										log.Printf("❌ Could not derive buyer's ATA: %v", err)
										continue
									}

									recentBlockhash, err := solanaRpcClient.GetLatestBlockhash(context.TODO(), rpc.CommitmentProcessed)
									if err != nil {
										log.Printf("❌ Failed to get fresh blockhash: %v", err)
										continue
									}

									const INITIAL_VIRTUAL_SOL_BUY = 30.0
									const INITIAL_VIRTUAL_TOKENS_BUY = 1073000000.0
									solDepositedInSOL := float64(initialSolLamports) / float64(LamportsPerSOL)
									k := INITIAL_VIRTUAL_SOL_BUY * INITIAL_VIRTUAL_TOKENS_BUY

									currentVirtualSol := INITIAL_VIRTUAL_SOL_BUY + solDepositedInSOL
									currentVirtualTokens := k / currentVirtualSol

									virtualSolAfterBuy := currentVirtualSol + BUY_AMOUNT_SOL
									virtualTokensAfterBuy := k / virtualSolAfterBuy

									tokensToBuy := currentVirtualTokens - virtualTokensAfterBuy
									tokenAmountToBuy := uint64(tokensToBuy * 1_000_000)
									maxSolCostLamports := uint64(BUY_AMOUNT_SOL * LamportsPerSOL * 1.20)

									buyInstructionData := make([]byte, 0, len(PUMPFUN_BUY_DISCRIMINATOR)+16)
									buyInstructionData = append(buyInstructionData, PUMPFUN_BUY_DISCRIMINATOR...)
									buf := new(bytes.Buffer)
									binary.Write(buf, binary.LittleEndian, tokenAmountToBuy)
									binary.Write(buf, binary.LittleEndian, maxSolCostLamports)
									buyInstructionData = append(buyInstructionData, buf.Bytes()...)

									txBuilder := solana.NewTransactionBuilder()

									txBuilder.AddInstruction(
										computebudget.NewSetComputeUnitLimitInstruction(400_000).Build(),
									).AddInstruction(
										computebudget.NewSetComputeUnitPriceInstruction(500_000).Build(),
									).AddInstruction(
										solana.NewInstruction(
											solana.SPLAssociatedTokenAccountProgramID,
											solana.AccountMetaSlice{
												{PublicKey: buyerAccount.PublicKey(), IsSigner: true, IsWritable: true},
												{PublicKey: buyerATA, IsSigner: false, IsWritable: true},
												{PublicKey: buyerAccount.PublicKey(), IsSigner: false, IsWritable: false},
												{PublicKey: mintKey, IsSigner: false, IsWritable: false},
												{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
												{PublicKey: solana.TokenProgramID, IsSigner: false, IsWritable: false},
											},
											[]byte{1},
										),
									)

									// Find the correct creator_vault PDA for the mint
									var creatorVaultPDA solana.PublicKey
									if !mintKey.IsZero() {
										pda, _, err := solana.FindProgramAddress(
											[][]byte{
												[]byte("vault"),
												mintKey.Bytes(),
											},
											pumpFunPK,
										)
										if err == nil {
											creatorVaultPDA = pda
										}
									}

									// Find the creator_vault account in the fullAccountList
									if !creatorVaultPDA.IsZero() {
										for _, accountBytes := range fullAccountList {
											pk := solana.PublicKeyFromBytes(accountBytes)
											if pk.Equals(creatorVaultPDA) {
												creatorVaultKey = pk
												break
											}
										}
									}

									txBuilder.AddInstruction(
										solana.NewInstruction(
											pumpProgramKey,
											solana.AccountMetaSlice{
												{PublicKey: globalKey, IsSigner: false, IsWritable: false},
												{PublicKey: feeRecipientPK, IsSigner: false, IsWritable: true},
												{PublicKey: mintKey, IsSigner: false, IsWritable: true},
												{PublicKey: bondingCurveKey, IsSigner: false, IsWritable: true},
												{PublicKey: associatedBondingCurveKey, IsSigner: false, IsWritable: true},
												{PublicKey: buyerATA, IsSigner: false, IsWritable: true},
												{PublicKey: buyerAccount.PublicKey(), IsSigner: true, IsWritable: true},
												{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
												{PublicKey: solana.TokenProgramID, IsSigner: false, IsWritable: false},
												{PublicKey: creatorVaultKey, IsSigner: false, IsWritable: true},
												{PublicKey: eventAuthorityKey, IsSigner: false, IsWritable: false},
												{PublicKey: pumpProgramKey, IsSigner: false, IsWritable: false},
											},
											buyInstructionData,
										),
									)

									txBuilder.SetFeePayer(buyerAccount.PublicKey())
									txBuilder.SetRecentBlockHash(recentBlockhash.Value.Blockhash)

									txBuy, err := txBuilder.Build()
									if err != nil {
										log.Printf("❌ Failed to build transaction: %v", err)
										continue
									}

									_, err = txBuy.Sign(
										func(key solana.PublicKey) *solana.PrivateKey {
											if buyerAccount.PublicKey().Equals(key) {
												return &buyerAccount
											}
											return nil
										},
									)
									if err != nil {
										log.Printf("❌ Failed to sign transaction: %v", err)
										continue
									}

									buySignature, err := solanaRpcClient.SendTransactionWithOpts(
										context.TODO(),
										txBuy,
										rpc.TransactionOpts{
											SkipPreflight:       true,
											PreflightCommitment: rpc.CommitmentProcessed,
										},
									)
									if err != nil {
										log.Printf("❌ Failed to send buy transaction: %v", err)
										continue
									}

									log.Printf("✅ Buy Transaction sent! Signature: %s", buySignature.String())
									log.Printf("🔍 View on Solscan: https://solscan.io/tx/%s", buySignature.String())
								}
							}
						}
					}
				}
			}
		}
	}
}
