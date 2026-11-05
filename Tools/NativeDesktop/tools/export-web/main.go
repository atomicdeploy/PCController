// Command export-web writes the reproducible static RayanLamp dashboard ZIP.
package main

import (
	"flag"
	"fmt"
	"os"

	"rayanlamp.local/app/tools/internal/webexport"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(arguments []string) int {
	flags := flag.NewFlagSet("export-web", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	source := flags.String("source", "", "compiled frontend distribution directory")
	output := flags.String("output", "", "destination ZIP path")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "export-web: unexpected argument %q\n", flags.Arg(0))
		return 2
	}
	if _, err := webexport.ExportDirectory(*source, *output); err != nil {
		fmt.Fprintf(os.Stderr, "export-web: %v\n", err)
		return 1
	}
	return 0
}
