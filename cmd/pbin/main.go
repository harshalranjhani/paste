package main

import (
	"os"

	"github.com/harshalranjhani/paste/internal/pbin"
)

var version string

func main() {
	os.Exit(pbin.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv, version))
}
