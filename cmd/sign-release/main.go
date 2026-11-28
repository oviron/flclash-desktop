// Command sign-release signs the sums of a release with the project's
// Ed25519 key, as the release workflow does. The app checks the signature
// against the public key built into it before it installs an update
// (internal/update).
//
//	FLCLASH_SIGNING_KEY=... sign-release SHA256SUMS   writes SHA256SUMS.sig
//	sign-release -new FILE                             writes a new private key
//	                                                   to FILE, prints the public one
//
// The private key is the base64 of its 32-byte seed.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/oviron/flclash-desktop/internal/update"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sign-release:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 2 && args[0] == "-new" {
		pub, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			return err
		}
		seed := base64.StdEncoding.EncodeToString(priv.Seed())
		if err := os.WriteFile(args[1], []byte(seed+"\n"), 0o600); err != nil {
			return err
		}
		fmt.Println(base64.StdEncoding.EncodeToString(pub))
		return nil
	}
	if len(args) != 1 {
		return errors.New("usage: FLCLASH_SIGNING_KEY=... sign-release FILE | sign-release -new KEYFILE")
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("FLCLASH_SIGNING_KEY")))
	if err != nil || len(seed) != ed25519.SeedSize {
		return errors.New("FLCLASH_SIGNING_KEY is not the base64 of a 32-byte seed")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	// A key that is not the one the app trusts would sign releases no copy
	// installs: better to stop the release.
	if got := base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)); got != update.PublicKey {
		return errors.New("the key is not the one the app trusts (update.PublicKey)")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data))
	return os.WriteFile(args[0]+".sig", []byte(sig+"\n"), 0o644)
}
