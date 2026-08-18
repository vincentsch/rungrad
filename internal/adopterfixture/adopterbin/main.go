package main

import (
	"os"

	"github.com/vincentsch/rungrad/internal/adopterfixture"
)

func main() {
	os.Exit(adopterfixture.NewApp().Run(os.Args[1:], os.Stdout, os.Stderr))
}
