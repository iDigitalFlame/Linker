package main

import (
	"flag"
	"fmt"
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
  -c <file>       Configuration file path. The environment
                  variable "LINKER_CONFIG" can be used to
                  specify the file path instead.
`

func main() {
	flagConfig := flag.String("c", "", "Configuration file path.")
	flagList := flag.Bool("l", false, "List the URL mapping and exit.")
	flagAdd := flag.String("a", "", "Add the specified <name> to <URL> mapping.")
	flagDelete := flag.String("r", "", "Delete the specified <name> to URL mapping.")
	flagDefault := flag.Bool("d", false, "Dump the default configuration and exit.")

	flag.Usage = func() {
		fmt.Println(usage)
		os.Exit(1)
	}
	flag.Parse()

	if *flagDefault {
		fmt.Println(linker.DefaultConfig)
		os.Exit(2)
	}

	l, err := linker.New(*flagConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
		os.Exit(1)
	}
	defer l.Close()

	if *flagList {
		if err := l.List(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
			os.Exit(1)
		}
		os.Exit(0)
	}
	if len(*flagAdd) > 0 {
		a := flag.Args()
		if a == nil || len(a) < 1 {
			fmt.Println(usage)
			os.Exit(1)
		}
		if err := l.Add(*flagAdd, a[0]); err != nil {
			fmt.Fprintf(os.Stderr, "Error %q: %s\n", a[0], err.Error())
			os.Exit(1)
		}
		fmt.Printf("Added mapping %q to %q!\n", *flagAdd, a[0])
		os.Exit(0)
	}
	if len(*flagDelete) > 0 {
		if err := l.Delete(*flagDelete); err != nil {
			fmt.Fprintf(os.Stderr, "Error removing %q: %s\n", *flagDelete, err.Error())
			os.Exit(1)
		}
		fmt.Printf("Deleted mapping %q!\n", *flagDelete)
		os.Exit(0)
	}
	if err := l.Listen(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
		os.Exit(1)
	}
}
