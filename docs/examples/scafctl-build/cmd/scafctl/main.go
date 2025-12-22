package main

import (
	"fmt"
	"os"

	"example.com/scafctl-sample/internal/version"
)

func main() {
	cmd := "help"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "version":
		fmt.Printf("scafctl-sample version %s\n", version.Version())
	default:
		printHelp()
	}
}

func printHelp() {
	fmt.Println("scafctl-sample")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  scafctl-sample <command>")
	fmt.Println()
	fmt.Println("Available Commands:")
	fmt.Println("  help       Show this help message")
	fmt.Println("  version    Print version information")
	fmt.Println()
	fmt.Println("Use \"scafctl-sample <command> --help\" for more information about a command.")
}
