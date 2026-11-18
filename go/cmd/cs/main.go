// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

// Command cs is the Carbyne Stack command line interface.
package main

import (
	"os"

	"github.com/Samrat7745/cli/go/internal/cli"
)

func main() { os.Exit(cli.Main()) }
