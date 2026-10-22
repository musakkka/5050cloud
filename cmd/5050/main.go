// SPDX-License-Identifier: MIT

// Command 5050 is the 5050cloud command line client. Installed as `railway`
// it also accepts Railway's verbs and flags.
package main

import (
	"os"

	"github.com/musakkka/5050cloud/internal/cli"
)

func main() { os.Exit(cli.Main()) }
