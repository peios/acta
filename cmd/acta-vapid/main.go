// Command acta-vapid mints a VAPID key pair for Web Push and prints it as the
// environment variables the server reads. Run it once per deployment and put
// the output in the server's environment (keep the private key secret):
//
//	go run ./cmd/acta-vapid
//
// Rotating the keys invalidates every existing push subscription (browsers must
// re-subscribe against the new public key), so do it deliberately.
package main

import (
	"fmt"
	"os"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func main() {
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate VAPID keys:", err)
		os.Exit(1)
	}
	fmt.Printf("ACTA_VAPID_PUBLIC_KEY=%s\n", pub)
	fmt.Printf("ACTA_VAPID_PRIVATE_KEY=%s\n", priv)
}
