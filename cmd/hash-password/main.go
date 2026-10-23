// Hash an operator-supplied secret from standard input, never a command argument.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 74))
	secret := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if err != nil || len(secret) < 6 || len(secret) > 72 {
		fmt.Fprintln(os.Stderr, "password must contain 6–72 bytes")
		os.Exit(1)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), 12)
	if err != nil {
		os.Exit(1)
	}
	fmt.Println(string(hash))
}
