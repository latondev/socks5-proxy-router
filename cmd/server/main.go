package main

import (
	"log"

	"socks5-pool/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatalf("[fatal] %v", err)
	}
}
