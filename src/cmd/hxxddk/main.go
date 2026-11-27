// Command hxxddk is the DevKit command-line client.
package main

import (
	"os"

	"github.com/hxx/hxxDiscourseDevKit/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
