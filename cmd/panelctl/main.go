package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"github.com/caoyanyi/k8s-panel/internal/buildinfo"
	"github.com/caoyanyi/k8s-panel/internal/secure"
)

func main() {
	if len(os.Args) != 2 {
		usage()
	}
	switch os.Args[1] {
	case "version", "--version":
		fmt.Println(buildinfo.String("panelctl"))
	case "encryption-key":
		key := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			fatal(err)
		}
		fmt.Println(base64.StdEncoding.EncodeToString(key))
	case "hash-password":
		input, err := io.ReadAll(io.LimitReader(os.Stdin, 1025))
		if err != nil {
			fatal(err)
		}
		if len(input) > 1024 {
			fatal(fmt.Errorf("password exceeds 1024 bytes"))
		}
		password := string(bytes.TrimRight(input, "\r\n"))
		hash, err := secure.NewPasswordHasher(secure.DefaultPasswordParams()).Hash(password)
		if err != nil {
			fatal(err)
		}
		fmt.Println(hash)
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: panelctl version | panelctl encryption-key | panelctl hash-password")
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "panelctl:", err)
	os.Exit(1)
}
