package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	prompt "github.com/c-bata/go-prompt" // Import the library
)

// Represents the state of our CLI (though very simple for this calculator)
type CalculatorCLI struct {
	// We could add modes here later if needed
}

// --- Executor Function ---
// This function takes the user's input string and processes it.
func (c *CalculatorCLI) executor(in string) {
	// Trim whitespace and split into words
	in = strings.TrimSpace(in)
	parts := strings.Fields(in)

	if len(parts) == 0 {
		return // Ignore empty input
	}

	command := strings.ToLower(parts[0])

	switch command {
	case "calculate":
		c.handleCalculate(parts[1:]) // Pass remaining parts as arguments
	case "exit", "quit":
		fmt.Println("Exiting calculator.")
		os.Exit(0)
	case "":
		// Ignore empty line
	default:
		fmt.Printf("Error: Unknown command '%s'\n", command)
	}
}

// Handles the 'calculate' command and its subcommands (operations)
func (c *CalculatorCLI) handleCalculate(args []string) {
	if len(args) < 3 {
		fmt.Println("Error: Insufficient arguments for calculate.")
		fmt.Println("Usage: calculate <add|sub|mul|div> <number1> <number2>")
		return
	}

	operation := strings.ToLower(args[0])
	num1Str := args[1]
	num2Str := args[2]

	// Convert string arguments to numbers (float64 for flexibility)
	num1, err1 := strconv.ParseFloat(num1Str, 64)
	num2, err2 := strconv.ParseFloat(num2Str, 64)

	if err1 != nil || err2 != nil {
		fmt.Printf("Error: Invalid number format. '%s' or '%s' is not a valid number.\n", num1Str, num2Str)
		return
	}

	// Perform the calculation based on the operation
	var result float64
	var err error = nil

	switch operation {
	case "add", "+":
		result = num1 + num2
	case "sub", "subtract", "-":
		result = num1 - num2
	case "mul", "multiply", "*":
		result = num1 * num2
	case "div", "divide", "/":
		if num2 == 0 {
			err = fmt.Errorf("division by zero")
		} else {
			result = num1 / num2
		}
	default:
		err = fmt.Errorf("unknown operation '%s'", operation)
	}

	// Print result or error
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		// Use %g to format float nicely (removes trailing zeros)
		fmt.Printf("Result: %g\n", result)
	}
}

// --- Completer Function ---
// This function provides suggestions for tab completion.
func (c *CalculatorCLI) completer(d prompt.Document) []prompt.Suggest {
	// Get the text before the cursor
	textBeforeCursor := d.TextBeforeCursor()
	// Split into words
	words := strings.Fields(textBeforeCursor)

	// If typing the first word or it's empty
	if len(words) == 0 || (len(words) == 1 && !strings.HasSuffix(textBeforeCursor, " ")) {
		s := []prompt.Suggest{
			{Text: "calculate", Description: "Perform calculation (add, sub, mul, div)"},
			{Text: "exit", Description: "Exit the calculator"},
			{Text: "quit", Description: "Exit the calculator"},
		}
		// Filter suggestions based on what's already typed
		return prompt.FilterHasPrefix(s, d.GetWordBeforeCursor(), true)
	}

	// If the first word is 'calculate' and we are typing the second word (the operation)
	if len(words) >= 1 && strings.ToLower(words[0]) == "calculate" && (len(words) == 1 || (len(words) == 2 && !strings.HasSuffix(textBeforeCursor, " "))) {
		s := []prompt.Suggest{
			{Text: "add", Description: "Addition (+)"},
			{Text: "sub", Description: "Subtraction (-)"},
			{Text: "subtract", Description: "Subtraction (-)"},
			{Text: "mul", Description: "Multiplication (*)"},
			{Text: "multiply", Description: "Multiplication (*)"},
			{Text: "div", Description: "Division (/)"},
			{Text: "divide", Description: "Division (/)"},
		}
		// Filter operation suggestions
		return prompt.FilterHasPrefix(s, d.GetWordBeforeCursor(), true)
	}

	// Could add suggestions for numbers, but that's less common/useful for completion
	// if len(words) >= 2 && strings.ToLower(words[0]) == "calculate" {
	//     // Suggest number format or nothing when typing operands
	// }

	// No suggestions in other cases
	return []prompt.Suggest{}
}

// --- Live Prefix Function (for Cisco-like prompt) ---
func (c *CalculatorCLI) changeLivePrefix() (string, bool) {
	// For this simple example, the prompt is static.
	// If we had modes, we could change it here.
	return "Calculator> ", true // Static prompt
}

func main() {
	fmt.Println("Simple Calculator CLI (Ctrl+D or 'exit' to quit)")

	// Create our calculator state instance
	calc := &CalculatorCLI{}

	// Start the prompt loop
	p := prompt.New(
		calc.executor,  // The function to execute input
		calc.completer, // The function to provide completions
		prompt.OptionTitle("calculator-prompt"),
		prompt.OptionPrefix("Calculator> "),            // Initial prefix
		prompt.OptionLivePrefix(calc.changeLivePrefix), // Function to dynamically set prefix
		prompt.OptionPrefixTextColor(prompt.Yellow),    // Example: Set prompt color
	)

	p.Run() // Run the prompt loop
}
