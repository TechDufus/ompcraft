package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/TechDufus/ompcraft/companion/internal/update"
)

// The maintainer commands below are deliberately absent from --help. They never
// print private key material.

// cmdReleaseKeygen: `ompcraft release-keygen --out FILE` writes a new base64
// 64-byte ed25519 private key to FILE (mode 0600, refusing an existing file)
// and prints only the public key.
func cmdReleaseKeygen(args []string) error {
	fset := flag.NewFlagSet("release-keygen", flag.ContinueOnError)
	out := fset.String("out", "", "file for the private key (must not exist)")
	if err := fset.Parse(args); err != nil {
		return &exitError{code: 2}
	}
	if *out == "" {
		return &exitError{code: 2, err: errors.New("release-keygen needs --out FILE")}
	}
	pub, priv, err := update.GenerateKey()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists; refusing to overwrite a key", *out)
		}
		return fmt.Errorf("cannot create %s: %w", *out, err)
	}
	if _, err := f.WriteString(priv + "\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(*out)
		return fmt.Errorf("cannot write %s: %w", *out, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(*out)
		return err
	}
	fmt.Println(pub)
	return nil
}

// cmdReleaseSign: `ompcraft release-sign FILE` reads the base64 private key from
// $OMPCRAFT_SIGNING_KEY and writes FILE.sig = base64(ed25519 signature of FILE).
func cmdReleaseSign(args []string) error {
	fset := flag.NewFlagSet("release-sign", flag.ContinueOnError)
	if err := fset.Parse(args); err != nil {
		return &exitError{code: 2}
	}
	if fset.NArg() != 1 {
		return &exitError{code: 2, err: errors.New("usage: release-sign FILE (the private key comes from $OMPCRAFT_SIGNING_KEY)")}
	}
	key := os.Getenv("OMPCRAFT_SIGNING_KEY")
	if key == "" {
		return errors.New("OMPCRAFT_SIGNING_KEY is not set")
	}
	data, err := os.ReadFile(fset.Arg(0))
	if err != nil {
		return err
	}
	sig, err := update.Sign(key, data)
	if err != nil {
		return err
	}
	return os.WriteFile(fset.Arg(0)+".sig", []byte(sig+"\n"), 0o644)
}
