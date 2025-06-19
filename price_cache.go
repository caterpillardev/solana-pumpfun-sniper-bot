package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

type PriceCache struct {
	mu    sync.RWMutex
	price float64
}

func NewPriceCache() *PriceCache {
	return &PriceCache{}
}

func (pc *PriceCache) Set(price float64) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.price = price
}

func (pc *PriceCache) Get() float64 {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return pc.price
}

func fetchSolPrice() (float64, error) {
	resp, err := http.Get("https://api.coingecko.com/api/v3/simple/price?ids=solana&vs_currencies=usd")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		Solana struct {
			USD float64 `json:"usd"`
		} `json:"solana"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	if result.Solana.USD == 0 {
		return 0, fmt.Errorf("coingecko returned zero price for SOL")
	}

	return result.Solana.USD, nil
}

func (pc *PriceCache) UpdatePricePeriodically() {
	fetch := func() {
		price, err := fetchSolPrice()
		if err != nil {
			log.Printf("CoinGecko price fetch failed: %v. Price not updated.", err)
			return
		}
		pc.Set(price)
		log.Printf("SOL Price updated: $%.2f", price)
	}
	fetch()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C
		fetch()
	}
}
