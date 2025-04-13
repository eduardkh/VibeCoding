package main

import (
	"bufio"  // Used only for fallback basic input
	"errors" // Used for error checking (errors.Is)
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chzyer/readline" // External dependency needed!
)

// --- Constants for Modes ---
const (
	ModeUserExec = iota + 1
	ModePrivExec
	ModeGlobalConfig
	ModeInterfaceConfig
)

// --- Custom Errors ---
var (
	ErrAmbiguousCommand  = fmt.Errorf("ambiguous command")
	ErrInvalidInput      = fmt.Errorf("invalid input")
	ErrIncompleteCommand = fmt.Errorf("incomplete command")
	ErrBadArguments      = fmt.Errorf("invalid input or arguments")
)

// --- Data Structures ---

type InterfaceConfig struct {
	IPAddress   string
	SubnetMask  string
	Status      string // e.g., "up", "down", "administratively down"
	AdminStatus string // e.g., "up", "down"
}

type RunningConfig struct {
	Hostname   string
	Interfaces map[string]*InterfaceConfig // Map interface name to its config
}

// CommandHandler defines the function signature for command handlers
// Corrected type definition: matches the signature of a method value.
type CommandHandler func(args []string) error

// Command definition for completer/help
type CommandDef struct {
	Name    string
	Handler CommandHandler // Uses the corrected type
	Modes   []int          // Which modes this command is valid in (first word)
}

// --- Simulator State ---

type CiscoDeviceSimulator struct {
	hostname         string
	mode             int
	runningConfig    *RunningConfig
	currentInterface string // Name of the interface if in INTERFACE_CONFIG mode
	commandHistory   []string
	readlineInstance *readline.Instance             // Stores the active readline instance
	definedCommands  map[string]*CommandDef         // Map command name to definition
	modeCommandMap   map[int]map[string]*CommandDef // Cache commands per mode
	startTime        time.Time
}

// --- Constructor ---

func NewCiscoDeviceSimulator(hostname string) *CiscoDeviceSimulator {
	sim := &CiscoDeviceSimulator{
		hostname: hostname,
		mode:     ModeUserExec,
		runningConfig: &RunningConfig{
			Hostname:   hostname,
			Interfaces: make(map[string]*InterfaceConfig),
		},
		currentInterface: "",
		definedCommands:  make(map[string]*CommandDef),
		modeCommandMap:   make(map[int]map[string]*CommandDef),
		startTime:        time.Now(),
	}
	// Call registerCommands *after* sim is initialized so methods like sim.doEnable are valid
	sim.registerCommands()
	sim.buildModeCommandMaps()
	return sim
}

// --- Register Commands (Assignments now type-check with corrected CommandHandler) ---

func (sim *CiscoDeviceSimulator) registerCommands() {
	commands := []*CommandDef{
		// User Exec
		{"enable", sim.doEnable, []int{ModeUserExec}},
		{"exit", sim.doExitQuit, []int{ModeUserExec, ModePrivExec}}, // Exit simulator
		{"quit", sim.doExitQuit, []int{ModeUserExec}},               // Often alias for exit

		// Priv Exec
		{"disable", sim.doDisable, []int{ModePrivExec}},
		{"configure", sim.doConfigure, []int{ModePrivExec}},
		{"show", sim.doShow, []int{ModePrivExec}},
		{"history", sim.showHistoryCmd, []int{ModePrivExec}}, // Added history command

		// Config Modes Base
		{"end", sim.doEnd, []int{ModeGlobalConfig, ModeInterfaceConfig}},
		// Use different 'exit' for modes vs simulator exit
		{"exit", sim.doExitMode, []int{ModeGlobalConfig, ModeInterfaceConfig}},

		// Global Config
		{"hostname", sim.doHostname, []int{ModeGlobalConfig}},
		{"interface", sim.doInterface, []int{ModeGlobalConfig}},
		{"no", sim.doNo, []int{ModeGlobalConfig, ModeInterfaceConfig}}, // 'no' applies in both

		// Interface Config
		{"ip", sim.doIP, []int{ModeInterfaceConfig}},
		{"shutdown", sim.doShutdown, []int{ModeInterfaceConfig}},

		// Help ('?' is handled specially in findCommandByAbbreviation)
	}

	// Populate the definedCommands map
	for _, cmd := range commands {
		// Handle potential duplicate registrations if command applies to multiple modes
		if _, exists := sim.definedCommands[cmd.Name]; !exists {
			sim.definedCommands[cmd.Name] = cmd // Use pointer directly
		} else {
			// If already defined, just add the mode if not present
			existingCmd := sim.definedCommands[cmd.Name]
			existingCmd.Modes = append(existingCmd.Modes, cmd.Modes...)
			// Simple deduplication (can be improved)
			modeSet := make(map[int]bool)
			uniqueModes := []int{}
			for _, m := range existingCmd.Modes {
				if !modeSet[m] {
					modeSet[m] = true
					uniqueModes = append(uniqueModes, m)
				}
			}
			existingCmd.Modes = uniqueModes
		}
	}
}

// Build map for faster lookup of commands per mode
func (sim *CiscoDeviceSimulator) buildModeCommandMaps() {
	for mode := ModeUserExec; mode <= ModeInterfaceConfig; mode++ {
		sim.modeCommandMap[mode] = make(map[string]*CommandDef)
	}
	// Populate mode-specific maps using the Modes field from CommandDef
	for _, cmdDef := range sim.definedCommands { // Iterate over the definitions map
		for _, mode := range cmdDef.Modes {
			if _, ok := sim.modeCommandMap[mode]; ok {
				sim.modeCommandMap[mode][cmdDef.Name] = cmdDef
			}
		}
	}
}

// --- Helper Functions ---

// Get the prompt string based on the current mode
func (sim *CiscoDeviceSimulator) getPrompt() string {
	host := sim.runningConfig.Hostname
	switch sim.mode {
	case ModeUserExec:
		return fmt.Sprintf("%s>", host)
	case ModePrivExec:
		return fmt.Sprintf("%s#", host)
	case ModeGlobalConfig:
		return fmt.Sprintf("%s(config)#", host)
	case ModeInterfaceConfig:
		return fmt.Sprintf("%s(config-if)#", host)
	default:
		return fmt.Sprintf("%s?>", host) // Fallback
	}
}

// Get valid command *names* for the current mode (used for completion/help)
func (sim *CiscoDeviceSimulator) getValidCommandsForMode() []string {
	commands := []string{}
	if modeMap, ok := sim.modeCommandMap[sim.mode]; ok {
		for name := range modeMap {
			commands = append(commands, name)
		}
	}
	// Add '?' manually as it's handled specially
	commands = append(commands, "?")
	sort.Strings(commands) // Keep commands sorted for display/completion predictability
	return commands
}

// Find command definition by abbreviation
func (sim *CiscoDeviceSimulator) findCommandByAbbreviation(userInput string) (*CommandDef, error) {
	userInputLower := strings.ToLower(userInput)
	availableCommands := sim.getValidCommandsForMode() // Get commands for *current* mode
	matches := []*CommandDef{}

	// Handle '?' directly
	if userInput == "?" {
		// Return a temporary CommandDef for help
		// Ensure the handler matches the corrected CommandHandler type
		return &CommandDef{Name: "?", Handler: sim.doHelp}, nil
	}

	for _, cmdName := range availableCommands {
		if cmdName == "?" {
			continue
		} // Skip '?' during normal abbreviation matching
		if strings.HasPrefix(strings.ToLower(cmdName), userInputLower) {
			// Look up the *full* command definition based on the matched name
			if cmdDef, ok := sim.definedCommands[cmdName]; ok { // Use the main map
				matches = append(matches, cmdDef)
			}
		}
	}

	if len(matches) == 1 {
		// Unique match found
		return matches[0], nil
	} else if len(matches) > 1 {
		// Ambiguous, check if input is an exact match for one of the options
		for _, match := range matches {
			if strings.ToLower(match.Name) == userInputLower {
				return match, nil // Exact match resolves ambiguity
			}
		}
		// Still ambiguous
		return nil, fmt.Errorf("%w: %s", ErrAmbiguousCommand, userInput)
	} else {
		// No command starts with this input
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, userInput)
	}
}

// Validate IP address format (basic IPv4)
func isValidIP(ipStr string) bool {
	// Use standard library for more robust validation
	return net.ParseIP(ipStr) != nil
}

// Normalize interface names (e.g., g -> GigabitEthernet, fa0/1 -> FastEthernet0/1)
func normalizeInterfaceName(typePart, numPart string) (string, error) {
	t := strings.ToLower(typePart)
	var base string
	if strings.HasPrefix(t, "g") {
		base = "GigabitEthernet"
	} else if strings.HasPrefix(t, "f") {
		base = "FastEthernet"
	} else if strings.HasPrefix(t, "e") { // Less common but possible
		base = "Ethernet"
	} else {
		return "", fmt.Errorf("invalid interface type: %s", typePart)
	}
	// Basic validation for number part (e.g., 0/0, 10/23)
	matched, _ := regexp.MatchString(`^\d+/\d+$`, numPart)
	if !matched {
		return "", fmt.Errorf("invalid interface number format: %s", numPart)
	}
	return fmt.Sprintf("%s%s", base, numPart), nil
}

// Helper to print the invalid input marker like Cisco IOS
func (sim *CiscoDeviceSimulator) printInvalidInputMarker(line string, badPart string) {
	markerPos := -1
	// Try to find case-insensitively where the bad part starts
	lowerLine := strings.ToLower(line)
	lowerBadPart := strings.ToLower(badPart)
	markerPos = strings.Index(lowerLine, lowerBadPart)

	if markerPos == -1 { // Fallback to first non-space character if not found
		markerPos = strings.IndexFunc(line, func(r rune) bool {
			return !strings.ContainsRune(" \t", r) // Find first non-whitespace
		})
		if markerPos == -1 {
			markerPos = 0
		} // Default to start if line is all whitespace?
	}

	fmt.Println("% Invalid input detected at '^' marker.")
	fmt.Printf("  %s\n", line)                            // Print original line
	fmt.Printf("  %s^\n", strings.Repeat(" ", markerPos)) // Print spaces and marker
}

// --- Command Handlers (Implementations) ---

// Display help based on current mode
func (sim *CiscoDeviceSimulator) doHelp(args []string) error {
	fmt.Println("Available commands in this context:")
	validCommands := sim.getValidCommandsForMode()
	for _, cmd := range validCommands {
		fmt.Printf("  %s\n", cmd)
	}
	fmt.Println("(Use TAB for completion, abbreviations are supported)")
	return nil
}

// Exit the simulator application
func (sim *CiscoDeviceSimulator) doExitQuit(args []string) error {
	// Check if called in an appropriate mode (User or Priv Exec)
	if sim.mode == ModeUserExec || sim.mode == ModePrivExec {
		fmt.Println("Exiting simulator.")
		os.Exit(0)
	} else {
		// Technically, findCommandByAbbreviation should prevent this
		return fmt.Errorf("exit/quit command not valid in this mode")
	}
	return nil // Will not be reached
}

// Exit the current configuration mode
func (sim *CiscoDeviceSimulator) doExitMode(args []string) error {
	switch sim.mode {
	case ModeGlobalConfig:
		sim.mode = ModePrivExec
	case ModeInterfaceConfig:
		sim.mode = ModeGlobalConfig
		sim.currentInterface = "" // Clear interface context when exiting if-mode
	default:
		// Should be prevented by command availability checks
		return fmt.Errorf("exit command not valid in this mode")
	}
	// Update prompt if using readline
	if sim.readlineInstance != nil {
		sim.readlineInstance.SetPrompt(sim.getPrompt())
	}
	return nil
}

// Return directly to Privileged EXEC mode
func (sim *CiscoDeviceSimulator) doEnd(args []string) error {
	if sim.mode == ModeGlobalConfig || sim.mode == ModeInterfaceConfig {
		sim.mode = ModePrivExec
		sim.currentInterface = "" // Clear interface context
	} else {
		// Should be prevented by command availability checks
		return fmt.Errorf("end command not valid in this mode")
	}
	// Update prompt if using readline
	if sim.readlineInstance != nil {
		sim.readlineInstance.SetPrompt(sim.getPrompt())
	}
	return nil
}

// Enter Privileged EXEC mode
func (sim *CiscoDeviceSimulator) doEnable(args []string) error {
	sim.mode = ModePrivExec
	// In a real device, this might prompt for a password
	// fmt.Println("% Password: ***** (simulated)") // Keep it simple
	// Update prompt if using readline
	if sim.readlineInstance != nil {
		sim.readlineInstance.SetPrompt(sim.getPrompt())
	}
	return nil
}

// Return to User EXEC mode
func (sim *CiscoDeviceSimulator) doDisable(args []string) error {
	sim.mode = ModeUserExec
	// Update prompt if using readline
	if sim.readlineInstance != nil {
		sim.readlineInstance.SetPrompt(sim.getPrompt())
	}
	return nil
}

// Enter Global Configuration mode
func (sim *CiscoDeviceSimulator) doConfigure(args []string) error {
	// Check for abbreviation 't' for 'terminal'
	if len(args) < 1 || !strings.HasPrefix(strings.ToLower(args[0]), "t") {
		// Use specific error type for better handling upstream
		return fmt.Errorf("%w: expecting 'configure terminal'", ErrIncompleteCommand)
	}
	sim.mode = ModeGlobalConfig
	fmt.Println("Enter configuration commands, one per line. End with CNTL/Z or 'end'.")
	// Update prompt if using readline
	if sim.readlineInstance != nil {
		sim.readlineInstance.SetPrompt(sim.getPrompt())
	}
	return nil
}

// Set the device hostname
func (sim *CiscoDeviceSimulator) doHostname(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%w: expecting 'hostname <name>'", ErrIncompleteCommand)
	}
	newName := args[0]
	// Basic validation (letters, numbers, hyphens, not starting/ending with hyphen)
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$`, newName)
	if !matched {
		return fmt.Errorf("invalid hostname format") // Return specific error
	}
	sim.runningConfig.Hostname = newName
	// Update prompt immediately if using readline
	if sim.readlineInstance != nil {
		sim.readlineInstance.SetPrompt(sim.getPrompt())
	}
	return nil
}

// Enter interface configuration mode
func (sim *CiscoDeviceSimulator) doInterface(args []string) error {
	if len(args) < 1 { // Need at least the type part
		return fmt.Errorf("%w: expecting 'interface <type><number>' or 'interface <type> <number>'", ErrIncompleteCommand)
	}
	// Join args in case user typed "g 0/0" vs "g0/0" - handles both styles
	intfInput := strings.Join(args, "")

	// Use regex to separate type abbreviation/name from number
	// Allows forms like: g0/0, gi0/0, GigabitEthernet0/0, f0/1, fa0/1
	// Make type part non-greedy, require number part strictly
	re := regexp.MustCompile(`(?i)^([a-z]+?)\s*(\d+/\d+)$`)
	match := re.FindStringSubmatch(intfInput)

	if match == nil || len(match) != 3 {
		// Try splitting if regex fails (e.g., "g 0/0" might be parsed as args ["g", "0/0"])
		if len(args) == 2 {
			match = []string{args[0] + args[1], args[0], args[1]} // Reconstruct match array
		} else {
			return fmt.Errorf("invalid interface format: %s. Expecting e.g., 'g0/0', 'FastEthernet0/1'", intfInput)
		}
	}

	intfTypePart := match[1]
	intfNumPart := match[2]

	intfName, err := normalizeInterfaceName(intfTypePart, intfNumPart)
	if err != nil {
		return err // Return normalization error
	}

	// Create interface entry if it doesn't exist
	if _, exists := sim.runningConfig.Interfaces[intfName]; !exists {
		sim.runningConfig.Interfaces[intfName] = &InterfaceConfig{
			Status:      "administratively down", // Initial state
			AdminStatus: "down",                  // Explicit admin state
		}
	}
	sim.currentInterface = intfName // Set context
	sim.mode = ModeInterfaceConfig  // Change mode
	// Update prompt immediately if using readline
	if sim.readlineInstance != nil {
		sim.readlineInstance.SetPrompt(sim.getPrompt())
	}
	return nil
}

// Handle 'ip' commands (currently just 'ip address')
func (sim *CiscoDeviceSimulator) doIP(args []string) error {
	if sim.currentInterface == "" {
		return fmt.Errorf("command must be run in interface configuration mode")
	}
	// Check for 'address' abbreviation
	if len(args) < 1 || !strings.HasPrefix(strings.ToLower(args[0]), "a") {
		return fmt.Errorf("%w: expecting 'ip address <ip> <subnet>'", ErrIncompleteCommand)
	}
	if len(args) != 3 { // Expecting 'address', ip, mask
		return fmt.Errorf("%w: expecting 'ip address <ip> <subnet>'", ErrBadArguments)
	}

	ipAddr, subnetMask := args[1], args[2]
	if !isValidIP(ipAddr) {
		return fmt.Errorf("invalid IP address format: %s", ipAddr)
	}
	if !isValidIP(subnetMask) { // Basic check, could add real subnet mask validation
		return fmt.Errorf("invalid subnet mask format: %s", subnetMask)
	}

	// Get current interface data
	intfData := sim.runningConfig.Interfaces[sim.currentInterface]
	intfData.IPAddress = ipAddr
	intfData.SubnetMask = subnetMask
	// Update operational status only if admin status is up
	if intfData.AdminStatus == "up" {
		intfData.Status = "up" // Interface comes up when IP is assigned (if not admin down)
	}
	return nil
}

// Administratively disable the current interface
func (sim *CiscoDeviceSimulator) doShutdown(args []string) error {
	if sim.currentInterface == "" {
		return fmt.Errorf("command must be run in interface configuration mode")
	}
	if len(args) > 0 {
		// 'shutdown' takes no arguments
		return fmt.Errorf("%w: 'shutdown' takes no arguments", ErrBadArguments)
	}

	intfData := sim.runningConfig.Interfaces[sim.currentInterface]
	intfData.Status = "administratively down"
	intfData.AdminStatus = "down" // Explicitly set admin status
	return nil
}

// Handle 'no' commands
func (sim *CiscoDeviceSimulator) doNo(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%w: 'no' command", ErrIncompleteCommand)
	}

	noSubCommandInput := args[0]
	subArgs := args[1:] // Remaining args for the sub-command

	// Determine possible commands that can follow 'no' in the current mode
	possibleNoCommands := []string{}
	if sim.mode == ModeInterfaceConfig {
		possibleNoCommands = []string{"shutdown", "ip"} // Add more as needed (e.g., 'switchport')
	} else if sim.mode == ModeGlobalConfig {
		possibleNoCommands = []string{"hostname", "interface"} // Add 'ip route' etc. if implemented
	} else {
		// Should be prevented by command availability check
		return fmt.Errorf("'no' command not applicable in this mode")
	}

	// Find the specific 'no' sub-command by abbreviation
	matchedSubCommand := ""
	ambiguous := false
	matches := []string{}
	for _, cmd := range possibleNoCommands {
		if strings.HasPrefix(strings.ToLower(cmd), strings.ToLower(noSubCommandInput)) {
			matches = append(matches, cmd)
		}
	}

	if len(matches) == 1 {
		matchedSubCommand = matches[0]
	} else if len(matches) > 1 {
		// Check for exact match among ambiguous options
		for _, m := range matches {
			if strings.ToLower(m) == strings.ToLower(noSubCommandInput) {
				matchedSubCommand = m
				break
			}
		}
		if matchedSubCommand == "" {
			// Still ambiguous if no exact match found
			ambiguous = true
		}
	}

	// Handle lookup results
	if ambiguous {
		return fmt.Errorf("%w: no %s", ErrAmbiguousCommand, noSubCommandInput)
	}
	if matchedSubCommand == "" {
		// No command matched the input
		return fmt.Errorf("%w: no %s", ErrInvalidInput, noSubCommandInput)
	}

	// --- Execute the specific 'no' action based on mode and matched command ---
	switch sim.mode {
	case ModeInterfaceConfig:
		switch matchedSubCommand {
		case "shutdown":
			return sim.noShutdown(subArgs) // Pass remaining args for validation
		case "ip":
			// Further check for 'address' abbreviation
			if len(subArgs) < 1 || !strings.HasPrefix(strings.ToLower(subArgs[0]), "a") {
				return fmt.Errorf("%w: expecting 'no ip address'", ErrIncompleteCommand)
			}
			return sim.noIPAddress(subArgs[1:]) // Pass args *after* 'address' for validation
		default:
			// Should not happen if possibleNoCommands is accurate
			return fmt.Errorf("internal error: unhandled 'no' subcommand '%s' in IF mode", matchedSubCommand)
		}
	case ModeGlobalConfig:
		switch matchedSubCommand {
		case "hostname":
			// You typically can't 'no' a hostname, you set a new one
			return fmt.Errorf("cannot 'no hostname'. Set a new one instead")
		case "interface":
			// 'no interface' isn't standard, maybe 'default interface'
			return fmt.Errorf("use 'default interface <name>' to reset an interface (not implemented)")
		default:
			// Should not happen
			return fmt.Errorf("internal error: unhandled 'no' subcommand '%s' in Global mode", matchedSubCommand)
		}
	}
	// Should not be reachable
	return fmt.Errorf("internal error: 'no' command reached unexpected state")
}

// Handle 'no shutdown'
func (sim *CiscoDeviceSimulator) noShutdown(args []string) error {
	if sim.currentInterface == "" {
		return fmt.Errorf("not in interface mode")
	} // Should be caught earlier
	if len(args) > 0 {
		// 'no shutdown' takes no arguments
		return fmt.Errorf("%w: 'no shutdown' takes no arguments", ErrBadArguments)
	}

	intfData := sim.runningConfig.Interfaces[sim.currentInterface]
	intfData.AdminStatus = "up" // Set admin status to up
	// Interface operational status only comes up if it has an IP or other necessary config
	// Simple simulation: comes up if IP exists, otherwise stays down (protocol down)
	if intfData.IPAddress != "" {
		intfData.Status = "up"
	} else {
		intfData.Status = "down"
	}
	return nil
}

// Handle 'no ip address'
func (sim *CiscoDeviceSimulator) noIPAddress(args []string) error {
	if sim.currentInterface == "" {
		return fmt.Errorf("not in interface mode")
	} // Should be caught earlier
	if len(args) > 0 {
		// 'no ip address' takes no further arguments
		return fmt.Errorf("%w: 'no ip address' takes no further arguments", ErrBadArguments)
	}

	intfData := sim.runningConfig.Interfaces[sim.currentInterface]
	intfData.IPAddress = ""  // Clear IP
	intfData.SubnetMask = "" // Clear mask
	// If admin status is up, the operational status goes down without an IP
	if intfData.AdminStatus == "up" {
		intfData.Status = "down"
	}
	// If admin status was down, status remains down or admin down.
	return nil
}

// Handle 'show' command variations
func (sim *CiscoDeviceSimulator) doShow(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%w: expecting 'show <subcommand>'", ErrIncompleteCommand)
	}
	subCommandInput := args[0]
	subArgs := args[1:] // Arguments for the subcommand

	// Define possible 'show' subcommands
	showOptions := []string{"version", "running-config", "run", "ip", "history"}

	// Find subcommand by abbreviation
	matches := []string{}
	for _, opt := range showOptions {
		if strings.HasPrefix(opt, strings.ToLower(subCommandInput)) {
			matches = append(matches, opt)
		}
	}

	matchedSubCommand := ""
	ambiguous := false
	if len(matches) == 1 {
		matchedSubCommand = matches[0] // Unique match
	} else if len(matches) > 1 {
		// Special case: 'run' is abbreviation for 'running-config'
		if strings.ToLower(subCommandInput) == "run" && contains(matches, "running-config") {
			matchedSubCommand = "running-config"
		} else {
			// Check for exact match among ambiguous options
			for _, m := range matches {
				if strings.ToLower(m) == strings.ToLower(subCommandInput) {
					matchedSubCommand = m
					break
				}
			}
			if matchedSubCommand == "" {
				// Still ambiguous if no exact match found
				ambiguous = true
			}
		}
	}

	// Handle lookup results
	if ambiguous {
		return fmt.Errorf("%w: show %s", ErrAmbiguousCommand, subCommandInput)
	}
	if matchedSubCommand == "" {
		return fmt.Errorf("%w: show %s", ErrInvalidInput, subCommandInput)
	}

	// Execute specific show command
	switch matchedSubCommand {
	case "version":
		return sim.showVersion(subArgs) // Pass remaining args for validation (should be none)
	case "running-config": // Also handles "run" due to logic above
		return sim.showRunningConfig(subArgs) // Pass remaining args for validation
	case "ip":
		// Further check for 'interface brief' using abbreviation
		if len(subArgs) < 1 || !strings.HasPrefix(strings.ToLower(subArgs[0]), "i") { // "interface"
			return fmt.Errorf("%w: expecting 'show ip interface brief'", ErrIncompleteCommand)
		}
		if len(subArgs) < 2 || !strings.HasPrefix(strings.ToLower(subArgs[1]), "b") { // "brief"
			return fmt.Errorf("%w: expecting 'show ip interface brief'", ErrIncompleteCommand)
		}
		// Check for extra args after 'brief'
		if len(subArgs) > 2 {
			return fmt.Errorf("%w: extra arguments after 'show ip interface brief'", ErrBadArguments)
		}
		return sim.showIPInterfaceBrief(subArgs[2:]) // Pass empty slice for validation
	case "history":
		return sim.showHistoryCmd(subArgs) // Pass remaining args for validation
	default:
		// Should not happen
		return fmt.Errorf("internal error: unhandled show command '%s'", matchedSubCommand)
	}
}

// Display simulated version info
func (sim *CiscoDeviceSimulator) showVersion(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: 'show version' takes no arguments", ErrBadArguments)
	}
	uptime := time.Since(sim.startTime)
	fmt.Println("Basic Cisco IOS Simulator (Go)")
	fmt.Printf("Version: 1.0 (Simulated)\n")
	fmt.Printf("Hostname: %s\n", sim.runningConfig.Hostname)
	// Format uptime like HH:MM:SS (approx)
	uptimeSeconds := int(uptime.Seconds())
	hours := uptimeSeconds / 3600
	minutes := (uptimeSeconds % 3600) / 60
	seconds := uptimeSeconds % 60
	fmt.Printf("Uptime: %02dh %02dm %02ds (Simulated)\n", hours, minutes, seconds)
	fmt.Println("\n(Limited information in this simulation)")
	return nil
}

// Helper to get sorting key for interface names
func sortInterfaceKey(intfName string) (int, int, int) {
	// Regex to extract type, slot, port
	re := regexp.MustCompile(`(?i)([a-z]+)(\d+)/(\d+)`)
	match := re.FindStringSubmatch(intfName)
	if match == nil || len(match) != 4 {
		return 999, 0, 0 // Fallback for non-standard names
	}
	typePrefix := strings.ToLower(match[1])
	slot, _ := strconv.Atoi(match[2]) // Ignore potential errors for simplicity
	port, _ := strconv.Atoi(match[3]) // Ignore potential errors

	// Assign weights for sorting order
	typeWeight := 99 // Default weight
	if strings.HasPrefix(typePrefix, "f") {
		typeWeight = 1
	} // FastEthernet
	if strings.HasPrefix(typePrefix, "g") {
		typeWeight = 2
	} // GigabitEthernet
	if strings.HasPrefix(typePrefix, "t") {
		typeWeight = 3
	} // TenGigabitEthernet

	return typeWeight, slot, port
}

// Display simulated running configuration
func (sim *CiscoDeviceSimulator) showRunningConfig(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: 'show running-config' takes no arguments", ErrBadArguments)
	}
	fmt.Println("Building configuration...")
	fmt.Println("!")
	fmt.Println("version 1.0 (Simulated)")
	fmt.Println("!")
	fmt.Printf("hostname %s\n", sim.runningConfig.Hostname)
	fmt.Println("!")

	// Get interface names and sort them
	intfNames := make([]string, 0, len(sim.runningConfig.Interfaces))
	for name := range sim.runningConfig.Interfaces {
		intfNames = append(intfNames, name)
	}
	// Use stable sort based on type, slot, port
	sort.SliceStable(intfNames, func(i, j int) bool {
		typeI, slotI, portI := sortInterfaceKey(intfNames[i])
		typeJ, slotJ, portJ := sortInterfaceKey(intfNames[j])
		if typeI != typeJ {
			return typeI < typeJ
		}
		if slotI != slotJ {
			return slotI < slotJ
		}
		return portI < portJ
	})

	// Print sorted interface configurations
	for _, intfName := range intfNames {
		intfData := sim.runningConfig.Interfaces[intfName]
		fmt.Printf("interface %s\n", intfName)
		if intfData.IPAddress != "" && intfData.SubnetMask != "" {
			fmt.Printf(" ip address %s %s\n", intfData.IPAddress, intfData.SubnetMask)
		}
		// Only show shutdown if admin status is explicitly down
		if intfData.AdminStatus == "down" {
			fmt.Println(" shutdown")
		}
		fmt.Println("!") // End of interface section
	}
	fmt.Println("!")
	fmt.Println("end")
	return nil
}

// Display brief summary of IP interfaces
func (sim *CiscoDeviceSimulator) showIPInterfaceBrief(args []string) error {
	// This handler is called *after* "show ip interface brief" is parsed
	// args should be empty here
	if len(args) > 0 {
		return fmt.Errorf("%w: extra arguments after 'show ip interface brief'", ErrBadArguments)
	}

	fmt.Println("Interface                  IP-Address      OK? Method Status                Protocol")

	// Get interface names and sort them
	intfNames := make([]string, 0, len(sim.runningConfig.Interfaces))
	for name := range sim.runningConfig.Interfaces {
		intfNames = append(intfNames, name)
	}
	sort.SliceStable(intfNames, func(i, j int) bool {
		typeI, slotI, portI := sortInterfaceKey(intfNames[i])
		typeJ, slotJ, portJ := sortInterfaceKey(intfNames[j])
		if typeI != typeJ {
			return typeI < typeJ
		}
		if slotI != slotJ {
			return slotI < slotJ
		}
		return portI < portJ
	})

	// Print formatted output for each interface
	for _, intfName := range intfNames {
		intfData := sim.runningConfig.Interfaces[intfName]
		ipAddr := "unassigned"
		ok := "NO "        // Note trailing space for alignment
		method := "unset " // Trailing space for alignment
		if intfData.IPAddress != "" {
			ipAddr = intfData.IPAddress
			ok = "YES"
			method = "manual"
		}
		status := intfData.Status
		protocol := status // Simple simulation: protocol status matches overall status

		// Use fmt.Printf with width specifiers for alignment
		fmt.Printf("%-26s %-15s %-3s %-6s %-21s %-s\n",
			intfName, ipAddr, ok, method, status, protocol)
	}
	return nil
}

// Display command history
func (sim *CiscoDeviceSimulator) showHistoryCmd(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: 'history' takes no arguments", ErrBadArguments)
	}
	fmt.Println("Command History:")
	// Use internal history slice
	historyLen := len(sim.commandHistory)
	for i := 0; i < historyLen; i++ {
		fmt.Printf(" %d: %s\n", i+1, sim.commandHistory[i])
	}
	return nil
}

// --- Process Command (Handles parsing, abbreviation, dispatch) ---

func (sim *CiscoDeviceSimulator) processCommand(line string) error {
	parts := strings.Fields(line) // Use Fields to handle multiple spaces better
	if len(parts) == 0 {
		return nil // Ignore empty lines
	}

	userCmdInput := parts[0]
	args := parts[1:] // Arguments passed to the handler

	// Find command by abbreviation (handles '?' specially)
	cmdDef, err := sim.findCommandByAbbreviation(userCmdInput)
	if err != nil {
		// Propagate specific known error types (Ambiguous, InvalidInput)
		// These will be handled in the run loop to show user-friendly messages
		return err
	}

	// Execute the handler
	if cmdDef.Handler != nil {
		// Call the handler method value (it's already bound to 'sim')
		// Pass the original arguments list directly
		handlerErr := cmdDef.Handler(args)
		if handlerErr != nil {
			// Return specific error types from handlers if they provide them
			// Otherwise, wrap as a general execution error maybe?
			// For now, let run loop print the handler's error message
			return handlerErr
		}
		return nil // Command executed successfully
	} else {
		// This case should only happen if findCommandByAbbreviation returns a
		// CommandDef with a nil handler, which shouldn't occur with current logic.
		return fmt.Errorf("command lookup failed for '%s' (internal error)", cmdDef.Name)
	}
}

// --- Run Loop (Main application loop using readline) ---

func (sim *CiscoDeviceSimulator) run() error {
	fmt.Println("--- Basic Cisco CLI Simulator (Go Version with Readline) ---")
	fmt.Println("Type '?' for help, 'exit' or 'quit' to leave.")

	// Configure readline completer using dynamic completions
	completer := readline.NewPrefixCompleter(
		readline.PcItemDynamic(sim.getDynamicCompletions), // Pass method value
	)

	// Initialize readline instance
	l, err := readline.NewEx(&readline.Config{
		Prompt:          sim.getPrompt(), // Initial prompt
		AutoComplete:    completer,       // Set our dynamic completer
		InterruptPrompt: "^C",            // Prompt on Ctrl+C
		EOFPrompt:       "exit",          // Simulate exit on Ctrl+D
		// HistoryFile:     "/tmp/cisco_sim_history.tmp", // Optional: persist history
		// HistorySearchFold: true,               // Optional: case-insensitive history search
	})
	if err != nil {
		// If readline fails (e.g., unsupported terminal), fall back to basic input
		fmt.Fprintf(os.Stderr, "Error initializing readline: %v. Falling back to basic input.\n", err)
		return sim.runBasic() // Call simpler loop without readline features
	}
	defer l.Close()           // Ensure readline is closed properly
	log.SetOutput(l.Stderr()) // Redirect log output to readline's stderr

	sim.readlineInstance = l // Store instance for prompt updates etc.

	// Main input loop
	for {
		l.SetPrompt(sim.getPrompt()) // Update prompt each time (handles hostname changes)
		line, err := l.Readline()    // Read input line

		// Handle readline errors
		if err == readline.ErrInterrupt { // Ctrl+C pressed
			// In Cisco, Ctrl+C usually just cancels the current line
			// The library handles clearing the line, we just continue
			continue
		} else if err == io.EOF { // Ctrl+D pressed
			// Treat EOF like 'exit' in base modes, or 'exit-mode' in config modes
			if sim.mode == ModeUserExec || sim.mode == ModePrivExec {
				fmt.Println("exit")     // Echo exit for clarity
				_ = sim.doExitQuit(nil) // Attempt to exit cleanly (ignore error as we exit anyway)
				return nil              // Exit run loop cleanly on EOF in base modes
			} else {
				// In config modes, act like 'exit' command to go up one level
				_ = sim.doExitMode(nil) // Ignore error, continue loop
				// Prompt will update on next iteration
			}
			continue // Get new prompt
		} else if err != nil {
			// Handle other potential readline errors
			log.Printf("Error reading line: %v\n", err)
			return err // Exit on unexpected errors
		}

		// Process valid input line
		line = strings.TrimSpace(line)
		if line == "" {
			continue // Ignore empty lines
		}

		// Add command to internal history (readline handles its own history)
		sim.commandHistory = append(sim.commandHistory, line)

		// Process the command using abbreviation logic
		processErr := sim.processCommand(line)

		// Handle specific errors returned by processCommand or handlers
		if processErr != nil {
			// *** CORRECTED ERROR HANDLING SCOPE ***
			if errors.Is(processErr, ErrAmbiguousCommand) {
				// Extract the ambiguous part from the error message
				errMsgParts := strings.SplitN(processErr.Error(), ": ", 2)
				ambiguousPart := ""
				if len(errMsgParts) == 2 {
					ambiguousPart = errMsgParts[1]
				} else {
					ambiguousPart = line
				} // Fallback
				fmt.Printf("%% Ambiguous command: \"%s\"\n", ambiguousPart)
			} else if errors.Is(processErr, ErrInvalidInput) {
				// Extract the invalid part from the error message
				errMsgParts := strings.SplitN(processErr.Error(), ": ", 2)
				invalidPart := ""
				if len(errMsgParts) == 2 {
					invalidPart = errMsgParts[1]
				} else {
					invalidPart = line
				} // Fallback
				sim.printInvalidInputMarker(line, invalidPart)
			} else if errors.Is(processErr, ErrIncompleteCommand) || errors.Is(processErr, ErrBadArguments) {
				// Print errors like incomplete command or bad args directly
				fmt.Printf("%% %v\n", processErr)
			} else {
				// Log other unexpected errors during processing
				log.Printf("Error processing command: %v\n", processErr)
			}
			// Continue the loop after handling user errors
		}
		// If processErr is nil, command succeeded, loop continues
	}
	// return nil // Should be unreachable if loop exits via return/break
}

// Fallback run loop if readline initialization fails
func (sim *CiscoDeviceSimulator) runBasic() error {
	fmt.Println("(Running with basic input - tab completion/history disabled)")
	reader := bufio.NewReader(os.Stdin) // Use standard buffered reader
	for {
		fmt.Print(sim.getPrompt())           // Print prompt manually
		line, err := reader.ReadString('\n') // Read until newline

		if err == io.EOF { // Ctrl+D
			fmt.Println("exit") // Simulate exit
			break               // Exit loop
		}
		if err != nil {
			log.Printf("Error reading input: %v\n", err)
			return err // Exit on other errors
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		sim.commandHistory = append(sim.commandHistory, line) // Keep internal history

		// Process command (same logic as before)
		processErr := sim.processCommand(line)
		// *** CORRECTED ERROR HANDLING SCOPE ***
		if processErr != nil {
			if errors.Is(processErr, ErrAmbiguousCommand) {
				errMsgParts := strings.SplitN(processErr.Error(), ": ", 2)
				ambiguousPart := ""
				if len(errMsgParts) == 2 {
					ambiguousPart = errMsgParts[1]
				} else {
					ambiguousPart = line
				} // Fallback
				fmt.Printf("%% Ambiguous command: \"%s\"\n", ambiguousPart)
			} else if errors.Is(processErr, ErrInvalidInput) {
				errMsgParts := strings.SplitN(processErr.Error(), ": ", 2)
				invalidPart := ""
				if len(errMsgParts) == 2 {
					invalidPart = errMsgParts[1]
				} else {
					invalidPart = line
				} // Fallback
				sim.printInvalidInputMarker(line, invalidPart)
			} else if errors.Is(processErr, ErrIncompleteCommand) || errors.Is(processErr, ErrBadArguments) {
				fmt.Printf("%% %v\n", processErr)
			} else {
				log.Printf("Error processing command: %v\n", processErr) // Log unexpected
			}
		}
	}
	return nil // Clean exit from basic loop
}

// --- Dynamic Completion Logic ---

// This function provides completions based on the current line context
func (sim *CiscoDeviceSimulator) getDynamicCompletions(line string) []string {
	parts := strings.Fields(line)                        // Split line into words
	currentModeCommands := sim.getValidCommandsForMode() // Base commands for current mode
	completions := []string{}                            // Slice to hold suggestions

	// Determine what's being completed based on cursor position / trailing space
	isStartingNewWord := strings.HasSuffix(line, " ")
	// cursorPos := len(line) // *** REMOVED UNUSED VARIABLE ***
	wordIndex := -1 // Index of the word being completed
	textToComplete := ""

	// Determine index and text based on whether we are starting a new word
	if isStartingNewWord {
		wordIndex = len(parts) // Completing the *next* word (index after last existing word)
		textToComplete = ""
	} else if len(parts) > 0 {
		wordIndex = len(parts) - 1 // Completing the *last* word typed
		textToComplete = parts[wordIndex]
	} else {
		wordIndex = 0 // Completing the very first word (line is empty or just starting)
		textToComplete = ""
	}

	// --- Completion Logic based on Word Index ---

	if wordIndex == 0 {
		// Completing the very first command word
		for _, cmd := range currentModeCommands {
			if strings.HasPrefix(cmd, textToComplete) {
				completions = append(completions, cmd)
			}
		}
	} else if len(parts) > 0 {
		// Completing arguments or subcommands (wordIndex >= 1)
		firstCmdInput := parts[0]
		// Find the full command for context (ignore errors here, best effort)
		cmdDef, _ := sim.findCommandByAbbreviation(firstCmdInput)
		baseCmd := ""
		if cmdDef != nil {
			baseCmd = cmdDef.Name
		}

		// --- Context-Specific Completion based on base command ---
		switch baseCmd {
		case "show":
			if sim.mode == ModePrivExec {
				completions = sim.completeShow(parts, textToComplete, wordIndex)
			}
		case "configure":
			if sim.mode == ModePrivExec && wordIndex == 1 { // Completing the word after 'configure'
				opt := "terminal"
				if strings.HasPrefix(opt, textToComplete) {
					completions = append(completions, opt)
				}
			}
		case "interface":
			if sim.mode == ModeGlobalConfig && wordIndex >= 1 { // Completing type or number
				completions = sim.completeInterface(parts, textToComplete, wordIndex)
			}
		case "ip":
			if sim.mode == ModeInterfaceConfig && wordIndex == 1 { // Completing word after 'ip'
				opt := "address"
				if strings.HasPrefix(opt, textToComplete) {
					completions = append(completions, opt)
				}
			}
			// Could add IP/subnet completion hints at wordIndex 2, 3
		case "no":
			// Complete the word immediately after 'no'
			if wordIndex == 1 {
				completions = sim.completeNoSubcommand(textToComplete)
			} else if wordIndex == 2 && len(parts) > 1 && strings.HasPrefix(parts[1], "ip") {
				// Complete subcommand after 'no ip'
				if sim.mode == ModeInterfaceConfig {
					opt := "address"
					if strings.HasPrefix(opt, textToComplete) {
						completions = append(completions, opt)
					}
				}
			}
		}
	}

	// Readline usually handles adding spaces, but we return the plain words.
	return completions
}

// Completion logic specifically for 'show' subcommands and args
func (sim *CiscoDeviceSimulator) completeShow(parts []string, text string, wordIndex int) []string {
	showOptions := []string{"version", "running-config", "run", "ip", "history"}
	ipOptions := []string{"interface"}
	ipIntOptions := []string{"brief"}
	completions := []string{}

	switch wordIndex {
	case 1: // Completing the word after 'show' (e.g., "run", "ip")
		for _, opt := range showOptions {
			if strings.HasPrefix(opt, text) {
				completions = append(completions, opt)
			}
		}
	case 2: // Completing word after 'show <cmd>'
		if strings.HasPrefix(parts[1], "ip") {
			for _, opt := range ipOptions {
				if strings.HasPrefix(opt, text) {
					completions = append(completions, opt)
				}
			}
		}
	case 3: // Completing word after 'show ip interface'
		if strings.HasPrefix(parts[1], "ip") && strings.HasPrefix(parts[2], "int") {
			for _, opt := range ipIntOptions {
				if strings.HasPrefix(opt, text) {
					completions = append(completions, opt)
				}
			}
		}
	}
	return completions
}

// Completion logic specifically for 'interface' arguments (type/number)
func (sim *CiscoDeviceSimulator) completeInterface(parts []string, text string, wordIndex int) []string {
	completions := []string{}
	types := []string{"GigabitEthernet", "FastEthernet", "g", "f", "gi", "fa"} // Common types/abbrevs
	existing := []string{}
	for name := range sim.runningConfig.Interfaces {
		existing = append(existing, name)
	}

	if wordIndex == 1 { // Completing the type/name part right after 'interface'
		possible := append(types, existing...)
		sort.Strings(possible)
		for _, opt := range possible {
			if strings.HasPrefix(strings.ToLower(opt), strings.ToLower(text)) {
				completions = append(completions, opt)
			}
		}
	} else if wordIndex >= 1 { // Potentially completing number part like 0/1 or full name
		currentPrefix := strings.Join(parts[1:wordIndex], "") // Join parts between 'interface' and current word
		currentPrefix += text                                 // Add text being typed

		foundExisting := false
		for _, name := range existing {
			if strings.HasPrefix(strings.ToLower(name), strings.ToLower(currentPrefix)) {
				completions = append(completions, name)
				foundExisting = true
			}
		}
		if !foundExisting && wordIndex == 1 && len(parts) > 1 && (strings.HasPrefix(parts[1], "g") || strings.HasPrefix(parts[1], "f")) {
			numPartRegex := regexp.MustCompile(`(\d+(?:/\d*)?)$`)
			numPartTyped := ""
			if match := numPartRegex.FindStringSubmatch(text); match != nil {
				numPartTyped = match[1]
			}
			for i := 0; i < 5; i++ {
				num := fmt.Sprintf("0/%d", i)
				if strings.HasPrefix(num, numPartTyped) {
					completions = append(completions, num)
				}
			}
		}
	}
	seen := make(map[string]bool)
	uniqueCompletions := []string{}
	for _, c := range completions {
		if !seen[c] {
			uniqueCompletions = append(uniqueCompletions, c)
			seen[c] = true
		}
	}
	return uniqueCompletions
}

// Completion logic specifically for the word *after* 'no'
func (sim *CiscoDeviceSimulator) completeNoSubcommand(text string) []string {
	completions := []string{}
	possibleNoCommands := []string{}
	// Determine commands based on current mode
	if sim.mode == ModeInterfaceConfig {
		possibleNoCommands = []string{"shutdown", "ip"}
	} else if sim.mode == ModeGlobalConfig {
		possibleNoCommands = []string{"hostname", "interface"}
	}
	// Suggest commands that start with the text being typed
	for _, opt := range possibleNoCommands {
		if strings.HasPrefix(opt, text) {
			completions = append(completions, opt)
		}
	}
	return completions
}

// --- Utility Functions ---

// Helper function to check if a slice contains a string
func contains(slice []string, item string) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

// --- Main Execution ---

func main() {
	simulator := NewCiscoDeviceSimulator("Router") // Initialize with default hostname
	err := simulator.run()                         // Start the main run loop
	if err != nil && !errors.Is(err, io.EOF) {     // Don't log EOF error as it's a clean exit path for runBasic
		log.Fatalf("Simulator exited with error: %v", err) // Log fatal errors
	}
	// Exit happens via os.Exit(0) or fatal error
}
