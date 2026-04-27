package main

import (
	"fmt"
	"os"

	"github.com/dejanr/video-transcribe/internal/app"
)

func main() {
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
