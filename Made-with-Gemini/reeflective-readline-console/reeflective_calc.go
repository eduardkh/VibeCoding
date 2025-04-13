package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	// Import the required libraries
	"github.com/reeflective/console"
	"github.com/spf13/cobra"

	// Import readline itself (used by console)

	// Use the standard 'x/term' package for terminal checks
	"golang.org/x/term"
)

// --- Cobra Command Definitions (Unchanged) ---

func performCalculation(operation string, args []string) {
	if len(args) != 2 {
		fmt.Println("Error: requires exactly two numbers.")
		return
	}
	num1Str, num2Str := args[0], args[1]
	num1, err1 := strconv.ParseFloat(num1Str, 64)
	num2, err2 := strconv.ParseFloat(num2Str, 64)
	if err1 != nil || err2 != nil {
		fmt.Printf("Error: Invalid number format. '%s' or '%s' is not a valid number.\n", num1Str, num2Str)
		return
	}
	var result float64
	var err error = nil
	switch operation {
	case "add":
		result = num1 + num2
	case "sub":
		result = num1 - num2
	case "mul":
		result = num1 * num2
	case "div":
		if num2 == 0 {
			err = fmt.Errorf("division by zero")
		} else {
			result = num1 / num2
		}
	default:
		err = fmt.Errorf("internal error: unknown operation %s", operation)
	}
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Result: %g\n", result)
	}
}

var addCmd = &cobra.Command{Use: "add <num1> <num2>", Short: "Adds two numbers", Args: cobra.ExactArgs(2), Run: func(cmd *cobra.Command, args []string) { performCalculation("add", args) }}
var subCmd = &cobra.Command{Use: "sub <num1> <num2>", Short: "Subtracts the second number from the first", Args: cobra.ExactArgs(2), Run: func(cmd *cobra.Command, args []string) { performCalculation("sub", args) }}
var mulCmd = &cobra.Command{Use: "mul <num1> <num2>", Short: "Multiplies two numbers", Args: cobra.ExactArgs(2), Run: func(cmd *cobra.Command, args []string) { performCalculation("mul", args) }}
var divCmd = &cobra.Command{Use: "div <num1> <num2>", Short: "Divides the first number by the second", Args: cobra.ExactArgs(2), Run: func(cmd *cobra.Command, args []string) { performCalculation("div", args) }}
var calculateCmd = &cobra.Command{Use: "calculate", Short: "Perform arithmetic calculations", Long: `Use subcommands add, sub, mul, div to perform calculations.`}
var exitCmd = &cobra.Command{Use: "exit", Short: "Exit the calculator shell", Aliases: []string{"quit"}, Run: func(cmd *cobra.Command, args []string) { fmt.Println("Exiting calculator."); os.Exit(0) }}

// --- Console Setup ---

func main() {
	// 1. Assemble the Cobra command structure
	calculateCmd.AddCommand(addCmd, subCmd, mulCmd, divCmd)
	rootCmd := &cobra.Command{Use: "calculator-cli"}
	rootCmd.AddCommand(calculateCmd, exitCmd)

	// 2. Create the reeflective console application
	app := console.New("calculator-cli")

	// 3. Bind the Cobra commands to the console's default menu
	getCmds := func() *cobra.Command { return rootCmd }
	defaultMenu := app.ActiveMenu()
	if defaultMenu != nil {
		defaultMenu.SetCommands(getCmds)
	} else {
		fmt.Fprintln(os.Stderr, "Warning: Could not get default active menu to set commands.")
	}

	// 4. Customize the Prompt
	promptString := "Calculator> "
	shell := app.Shell()
	if shell != nil && shell.Prompt != nil {
		// *** CORRECTED PROMPT SETTING ***
		// Define a function that returns the prompt string
		getPromptFunc := func() string {
			// In a more complex app, this function could dynamically build the prompt
			// based on current state (like mode, directory, etc.)
			return promptString
		}
		// Call the Primary field, passing the function, based on compiler error type `func(prompt func() string)`
		shell.Prompt.Primary(getPromptFunc)

	} else {
		fmt.Fprintln(os.Stderr, "Warning: Could not get shell or prompt instance to configure prompt.")
	}

	fmt.Println("Simple Calculator CLI (using reeflective/console)")
	fmt.Println("Type 'calculate <op> <n1> <n2>', 'help', or 'exit'. Tab completion is available.")

	// 5. Check if running in a TTY before starting interactive loop
	stdinFd := int(os.Stdin.Fd())
	stdoutFd := int(os.Stdout.Fd())
	if !term.IsTerminal(stdinFd) || !term.IsTerminal(stdoutFd) {
		fmt.Fprintln(os.Stderr, "Error: Application must be run in an interactive terminal.")
		os.Exit(1)
	}

	// 6. Start the interactive console loop
	err := app.Start()
	if err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(os.Stderr, "Error running console: %v\n", err)
		os.Exit(1)
	}
}

// Helper function (if needed)
func contains(slice []string, item string) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}
