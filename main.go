package main

import (
	"github.com/andreibanu/pusher/cmd"
)

var version = "dev"

func main() {
	cmd.Execute(version)
}
