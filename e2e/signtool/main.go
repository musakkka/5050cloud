// SPDX-License-Identifier: AGPL-3.0-only

// Command signtool makes and checks minisign signatures without the
// minisign binary. goreleaser calls it to sign checksums.txt, with the test
// key from e2e/keys for snapshots and with the MINISIGN_KEY secret for
// tagged releases (docs/SPEC.md 17.2 and 17.4).
//
//	signtool keygen -p key.pub -s key.sec
//	signtool sign   -s key.sec|-env MINISIGN_KEY -m FILE [-x FILE.minisig] [-t "trusted comment"]
//	signtool verify -p key.pub|-P BASE64 -m FILE [-x FILE.minisig]
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/musakkka/5050cloud/internal/upgrade/minisign"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "signtool:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("name a command: keygen, sign or verify")
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	pubPath := fs.String("p", "", "public key file")
	pubB64 := fs.String("P", "", "public key as one base64 line")
	secPath := fs.String("s", "", "secret key file")
	secEnv := fs.String("env", "", "environment variable that holds the secret key")
	msg := fs.String("m", "", "file to sign or verify")
	sigPath := fs.String("x", "", "signature file (default: FILE.minisig)")
	comment := fs.String("t", "", "trusted comment (default: timestamp and file name)")
	force := fs.Bool("f", false, "overwrite existing key files")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *sigPath == "" && *msg != "" {
		*sigPath = *msg + ".minisig"
	}
	switch args[0] {
	case "keygen":
		if *pubPath == "" || *secPath == "" {
			return errors.New("keygen needs -p and -s")
		}
		if !*force {
			for _, p := range []string{*pubPath, *secPath} {
				if _, err := os.Stat(p); err == nil {
					return fmt.Errorf("%s already exists; pass -f to overwrite it", p)
				}
			}
		}
		pub, sec, err := minisign.GenerateKey(nil)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*secPath, []byte(sec.File()), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(*pubPath, []byte(pub.File()), 0o644); err != nil {
			return err
		}
		fmt.Println(pub.String())
		return nil
	case "sign":
		var keyText string
		switch {
		case *secEnv != "":
			keyText = os.Getenv(*secEnv)
			if keyText == "" {
				return fmt.Errorf("the environment variable %s is empty", *secEnv)
			}
		case *secPath != "":
			b, err := os.ReadFile(*secPath)
			if err != nil {
				return err
			}
			keyText = string(b)
		default:
			return errors.New("sign needs -s FILE or -env NAME")
		}
		key, err := minisign.ParsePrivateKey(keyText)
		if err != nil {
			return err
		}
		if *msg == "" {
			return errors.New("sign needs -m FILE")
		}
		data, err := os.ReadFile(*msg)
		if err != nil {
			return err
		}
		tc := *comment
		if tc == "" {
			tc = fmt.Sprintf("timestamp:%d\tfile:%s", time.Now().Unix(), filepath.Base(*msg))
		}
		sig, err := minisign.Sign(key, data, tc)
		if err != nil {
			return err
		}
		return os.WriteFile(*sigPath, []byte(sig), 0o644)
	case "verify":
		keyText := *pubB64
		if keyText == "" {
			if *pubPath == "" {
				return errors.New("verify needs -p FILE or -P KEY")
			}
			b, err := os.ReadFile(*pubPath)
			if err != nil {
				return err
			}
			keyText = string(b)
		}
		pk, err := minisign.ParsePublicKey(keyText)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(*msg)
		if err != nil {
			return err
		}
		sig, err := os.ReadFile(*sigPath)
		if err != nil {
			return err
		}
		_, s, err := minisign.Verify([]minisign.PublicKey{pk}, data, sig)
		if err != nil {
			return err
		}
		fmt.Printf("Signature and comment signature verified\nTrusted comment: %s\n", s.TrustedComment)
		return nil
	}
	return fmt.Errorf("unknown command %q; use keygen, sign or verify", args[0])
}
