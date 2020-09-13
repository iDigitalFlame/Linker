// Copyright (C) 2020 iDigitalFlame
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//

package main

import (
	"flag"
	"os"

	"github.com/iDigitalFlame/linker"
)

const usage = `Linker - HTTP Web URL Shortener v2
iDigitalFlame 2020 (idigitalflame.com)

Usage:
  -h              Print this help menu.
  -l              List the URL mapping and exit.
  -d              Dump the default configuration and exit.
  -a <name> <URL> Add the specified <name> to <URL> mapping.
  -r <name>       Delete the specified <name> to URL mapping.
  -c <file>       Configuration file path. The environment variable
                  "LINKER_CONFIG" can be used to specify the file path instead.
`

func main() {
	os.Exit(mainFunc())
}
func mainFunc() int {
	var (
		args                = flag.NewFlagSet("Linker - HTTP Web URL Shortener v2", flag.ExitOnError)
		list, dump          bool
		add, delete, config string
	)
	args.Usage = func() {
		os.Stderr.WriteString(usage)
		os.Exit(2)
	}
	args.StringVar(&config, "c", "", "Configuration file path.")
	args.BoolVar(&list, "l", false, "List the URL mapping and exit.")
	args.BoolVar(&dump, "d", false, "Dump the default configuration and exit.")
	args.StringVar(&add, "a", "", "Add the specified <name> to <URL> mapping.")
	args.StringVar(&delete, "r", "", "Delete the specified <name> to URL mapping.")

	if err := args.Parse(os.Args[1:]); err != nil {
		os.Stderr.WriteString(usage)
		return 2
	}

	if dump {
		os.Stdout.WriteString(linker.DefaultConfig)
		return 0
	}

	l, err := linker.New(config)
	if err != nil {
		os.Stdout.WriteString("Error: " + err.Error() + "!\n")
		return 1
	}
	defer l.Close()

	switch {
	case list:
		if err := l.List(); err != nil {
			os.Stdout.WriteString("Error: " + err.Error() + "!\n")
			return 1
		}
		return 0
	case len(add) > 0:
		a := args.Args()
		if len(a) < 1 {
			os.Stderr.WriteString(usage)
			return 2
		}
		if err := l.Add(add, a[0]); err != nil {
			os.Stdout.WriteString(`Error adding "` + a[0] + `": ` + err.Error() + "!\n")
			return 1
		}
		os.Stdout.WriteString(`Added mapping "` + add + `" to "` + a[0] + `"!` + "\n")
		return 0
	case len(delete) > 0:
		if err := l.Delete(delete); err != nil {
			os.Stdout.WriteString(`Error removing "` + delete + `": ` + err.Error() + "!\n")
			return 1
		}
		os.Stdout.WriteString(`Deleted mapping "` + delete + `"!` + "\n")
		return 0
	default:
		if err := l.Listen(); err != nil {
			os.Stdout.WriteString("Error: " + err.Error() + "!\n")
			return 1
		}
	}
	return 0
}
