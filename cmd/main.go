// Copyright (C) 2020 - 2023 iDigitalFlame
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
	"errors"
	"flag"
	"os"

	"github.com/iDigitalFlame/linker"
)

var version = "unknown"

const usage = `Linker - HTTP Web URL Shortener v3
iDigitalFlame & PurpleSec 2020 - 2023 (idigitalflame.com)

Usage:
  -h              Print this help menu.
  -V              Print version string and exit.
  -l              List the URL mapping and exit.
  -s              Start the Linker HTTP service.
  -d              Dump the default configuration and exit.
  -a <name> <URL> Add the specified <name> to <URL> mapping.
  -r <name>       Delete the specified <name> to URL mapping.
  -c <file>       Configuration file path. The environment variable
                  "LINKER_CONFIG" can be used to specify the file path instead.
`

func main() {
	var (
		args                    = flag.NewFlagSet("Linker - HTTP Web URL Shortener v3_"+version, flag.ExitOnError)
		add, del, config        string
		list, dump, listen, ver bool
	)
	args.Usage = func() {
		os.Stderr.WriteString(usage)
		os.Exit(2)
	}
	args.StringVar(&config, "c", "", "")
	args.BoolVar(&list, "l", false, "")
	args.BoolVar(&listen, "s", false, "")
	args.BoolVar(&dump, "d", false, "")
	args.StringVar(&add, "a", "", "")
	args.StringVar(&del, "r", "", "")
	args.BoolVar(&ver, "V", false, "")

	if err := args.Parse(os.Args[1:]); err != nil {
		os.Stderr.WriteString(usage)
		os.Exit(2)
	}

	if ver {
		os.Stdout.WriteString("Linker: " + version + "\n")
		os.Exit(0)
	}

	if dump {
		os.Stdout.WriteString(linker.Defaults)
		os.Exit(0)
	}

	l, err := linker.New(config)
	if err != nil {
		os.Stdout.WriteString("Error: " + err.Error() + "!\n")
		os.Exit(1)
	}

	switch {
	case list:
		err = l.List()
	case listen:
		err = l.Listen()
	case len(add) > 0:
		a := args.Args()
		if len(a) < 1 {
			err = flag.ErrHelp
			break
		}
		if err = l.Add(add, a[0]); err != nil {
			err = errors.New(`adding "` + a[0] + `": ` + err.Error())
			break
		}
		os.Stdout.WriteString(`Added mapping "` + add + `" to "` + a[0] + `"!` + "\n")
	case len(del) > 0:
		if err = l.Delete(del); err != nil {
			err = errors.New(`removing "` + del + `": ` + err.Error())
			break
		}
		os.Stdout.WriteString(`Deleted mapping "` + del + `"!` + "\n")
	default:
		err = flag.ErrHelp
	}

	if l.Close(); err == flag.ErrHelp {
		os.Stdout.WriteString(usage)
		os.Exit(2)
	} else if err != nil {
		os.Stderr.WriteString("Error: " + err.Error() + "!\n")
		os.Exit(1)
	}
}
