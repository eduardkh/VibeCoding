#!/usr/bin/env python3

import sys
import time
import re
import inspect  # Added for better error checking

# --- Attempt to use readline ---
try:
    import readline
    READLINE_AVAILABLE = True
except ImportError:
    print("Readline library not found or failed to import. Tab completion disabled.")
    READLINE_AVAILABLE = False

# --- Constants for Modes ---
USER_EXEC = 1
PRIV_EXEC = 2
GLOBAL_CONFIG = 3
INTERFACE_CONFIG = 4

# --- Custom Exceptions ---


class AmbiguousCommandError(Exception):
    pass


class InvalidInputError(Exception):
    pass

# --- Tab Completion Logic (CiscoCompleter class - unchanged from previous version) ---


class CiscoCompleter:
    """Handles command completion based on simulator state."""

    def __init__(self, simulator):
        self.simulator = simulator
        self.matches = []

    def _get_available_commands(self):
        """Get commands relevant to the current mode."""
        # Use the simulator's new helper for consistency
        return sorted(self.simulator._get_valid_commands_for_mode())

    def _get_show_completions(self, line_parts):
        """Completions for 'show ...'"""
        # Allow abbreviation in completion suggestions too
        show_options = ['version', 'running-config', 'run', 'ip', 'history']
        if len(line_parts) == 2:  # Completing 'show <word>'
            current_text = line_parts[1]
            return [s + ' ' for s in show_options if s.startswith(current_text)]
        # Completing 'show ip <word>'
        elif len(line_parts) == 3 and line_parts[1].startswith('ip'):
            current_text = line_parts[2]
            ip_options = ['interface']
            return [s + ' ' for s in ip_options if s.startswith(current_text)]
        # Completing 'show ip interface <word>'
        elif len(line_parts) == 4 and line_parts[1].startswith('ip') and line_parts[2].startswith('int'):
            current_text = line_parts[3]
            ip_int_options = ['brief']
            return [s + ' ' for s in ip_int_options if s.startswith(current_text)]
        return []

    def _get_interface_completions(self, line_parts):
        """Completions for 'interface ...' or arguments"""
        # Suggest interface types or existing names
        if len(line_parts) == 2:
            current_text = line_parts[1]
            types = ['GigabitEthernet', 'FastEthernet']
            existing = list(self.simulator.running_config['interfaces'].keys())
            possible = types + existing
            # Add / hint for types
            # Suggest abbreviations too
            abbreviations = ['g', 'gi', 'f', 'fa']
            possible.extend(abbreviations)
            possible = sorted(list(set(possible)))  # Unique and sorted

            completions = []
            for s in possible:
                if s.startswith(current_text):
                    # Add space for full types, / for abbreviations needing numbers
                    suffix = ' '
                    if s.lower() in ['g', 'gi', 'f', 'fa']:
                        suffix = '/'  # Hint that number is needed
                    elif s in types:
                        suffix = ' '  # Full type name
                    elif s in existing:
                        suffix = ' '  # Existing interface name

                    # Avoid suggesting 'g' if user typed 'gi' already
                    is_more_specific_match = any(p.startswith(s) and len(p) > len(
                        s) for p in possible if p.startswith(current_text))
                    if not (s in abbreviations and is_more_specific_match and len(s) < len(current_text)):
                        completions.append(s + suffix)

            return list(set(completions))  # Unique suggestions

        elif len(line_parts) > 2:  # Potentially completing number part
            intf_type_part = line_parts[1]
            current_text = line_parts[-1]  # Text being completed

            # Check if previous part looks like a type abbreviation or start
            if intf_type_part.lower().startswith(('g', 'f')):
                # Suggest common numbers or complete existing full names
                # Reconstruct potential base name
                base_name = "".join(line_parts[1:-1])
                existing = list(
                    self.simulator.running_config['interfaces'].keys())
                completions = []

                # Suggest simple numbers if '/' was just typed or part of current text
                if line_parts[-2].endswith('/') or '/' in current_text:
                    # Extract number part being typed
                    num_part_match = re.search(
                        r"(\d+(?:/\d*)?)$", current_text)
                    num_part_typed = num_part_match.group(
                        1) if num_part_match else ""

                    for i in range(5):  # Suggest 0/0 to 0/4
                        num = f"0/{i}"
                        if num.startswith(num_part_typed):
                            # Append the part of num not typed yet
                            completions.append(num[len(num_part_typed):] + ' ')

                # Suggest completions for existing interfaces if matching type/start
                # Reconstruct potential full name start based on input
                potential_start = intf_type_part  # Start with the first part
                if len(line_parts) > 2:
                    # Add middle parts if any
                    potential_start += "".join(line_parts[2:-1])
                potential_start += current_text  # Add the part being completed

                for name in existing:
                    # Check if existing name starts with the reconstructed potential name
                    if name.lower().startswith(potential_start.lower()):
                        # Offer the rest of the name as completion
                        completion_text = name[len(potential_start):] + ' '
                        if completion_text:  # Avoid empty completions
                            completions.append(completion_text)

                return list(set(completions))  # Unique suggestions
        return []

    def _get_ip_completions(self, line_parts):
        """Completions for 'ip ...'"""
        if len(line_parts) == 2:
            current_text = line_parts[1]
            options = ['address']
            return [s + ' ' for s in options if s.startswith(current_text)]
        # Could add hints for IP/subnet format later if needed
        return []

    def _get_no_completions(self, line_parts):
        """Completions for 'no ...'"""
        if self.simulator.mode == INTERFACE_CONFIG:
            options = ['shutdown', 'ip']
            if len(line_parts) == 2:
                current_text = line_parts[1]
                return [s + ' ' for s in options if s.startswith(current_text)]
            elif len(line_parts) == 3 and line_parts[1].startswith('ip'):
                current_text = line_parts[2]
                ip_options = ['address']
                return [s + ' ' for s in ip_options if s.startswith(current_text)]
        elif self.simulator.mode == GLOBAL_CONFIG:
            options = ['hostname']  # Example if 'no hostname' was valid
            if len(line_parts) == 2:
                current_text = line_parts[1]
                return [s + ' ' for s in options if s.startswith(current_text)]

        return []

    def complete(self, text, state):
        """This is the completer function called by readline."""
        line = readline.get_line_buffer()
        # Use regex to split, handling multiple spaces better
        line_parts = re.split(r'\s+', line.strip())
        if line.endswith(' '):
            line_parts.append('')  # Add empty string if ending with space

        # If the line is empty or the cursor is at the beginning of a word
        if not line_parts or line_parts[-1] == '':
            current_text = ""
            completing_new_word = True
        else:
            current_text = line_parts[-1]
            completing_new_word = False

        if state == 0:
            # This is the first time for this completion, compute matches
            self.matches = []
            available_cmds = self._get_available_commands()

            # --- Context-Specific Completion ---
            if len(line_parts) <= 1:
                # Completing the very first command word
                self.matches = [
                    cmd + ' ' for cmd in available_cmds if cmd.startswith(current_text)]

            # Check first word abbreviation for context
            elif len(line_parts) > 1:
                first_cmd_input = line_parts[0]
                possible_first_cmds = [
                    cmd for cmd in available_cmds if cmd.startswith(first_cmd_input)]
                matched_first_cmd = possible_first_cmds[0] if len(
                    possible_first_cmds) == 1 else None

                if matched_first_cmd == 'show' and self.simulator.mode == PRIV_EXEC:
                    self.matches = self._get_show_completions(line_parts)

                elif matched_first_cmd == 'configure' and self.simulator.mode == PRIV_EXEC:
                    # Complete 'terminal' after 'configure' or abbreviation
                    if len(line_parts) == 2:
                        options = ['terminal']
                        self.matches = [
                            opt + ' ' for opt in options if opt.startswith(current_text)]

                elif matched_first_cmd == 'interface' and self.simulator.mode == GLOBAL_CONFIG:
                    self.matches = self._get_interface_completions(line_parts)

                elif matched_first_cmd == 'hostname' and self.simulator.mode == GLOBAL_CONFIG:
                    pass  # No standard completions for hostname value

                elif matched_first_cmd == 'ip' and self.simulator.mode == INTERFACE_CONFIG:
                    self.matches = self._get_ip_completions(line_parts)

                elif matched_first_cmd == 'no':
                    self.matches = self._get_no_completions(line_parts)

                # Add more context specific completions here

            # --- Generic Completion (Fallback) ---
            # If no specific context matched and completing a new word, offer base commands
            if not self.matches and completing_new_word:
                self.matches = [
                    cmd + ' ' for cmd in available_cmds if cmd.startswith(current_text)]

        # Return the match for the current state
        try:
            return self.matches[state]
        except IndexError:
            return None


class CiscoDeviceSimulator:
    """Simulates a basic Cisco IOS CLI."""

    def __init__(self, hostname="Router"):
        self.hostname = hostname
        self.mode = USER_EXEC
        self.running_config = {
            'hostname': hostname,
            'interfaces': {}  # Format: {'GigabitEthernet0/0': {'ip_address': ..., 'subnet_mask': ..., 'status': ..., 'admin_status': ...}}
        }
        self.current_interface = None
        self.command_history = []

        # Command Definitions (Handlers for base commands)
        # Note: Multi-word commands like 'show run' are handled in process_command/do_show
        self.commands = {
            USER_EXEC: {
                'enable': self.do_enable,
                'exit': self.do_exit_quit,
                'quit': self.do_exit_quit,
                '?': self.do_help,
            },
            PRIV_EXEC: {
                'disable': self.do_disable,
                'exit': self.do_exit_quit,
                'end': self.do_end,  # Technically 'end' is more for config modes
                'configure': self.do_configure,
                'show': self.do_show,
                '?': self.do_help,
                'history': self.show_history,  # Add history directly here
            },
            GLOBAL_CONFIG: {
                'exit': self.do_exit_mode,
                'end': self.do_end,
                'hostname': self.do_hostname,
                'interface': self.do_interface,
                'no': self.do_no,
                '?': self.do_help,
            },
            INTERFACE_CONFIG: {
                'exit': self.do_exit_mode,
                'end': self.do_end,
                'ip': self.do_ip,
                'shutdown': self.do_shutdown,
                'no': self.do_no,
                '?': self.do_help,
            }
        }

        # --- Setup Readline Completion (if available) ---
        if READLINE_AVAILABLE:
            self.completer = CiscoCompleter(self)
            readline.set_completer(self.completer.complete)
            readline.set_completer_delims(' \t\n;')
            readline.parse_and_bind('tab: complete')

    def get_prompt(self):
        # (Unchanged)
        host = self.running_config['hostname']
        if self.mode == USER_EXEC:
            return f"{host}>"
        if self.mode == PRIV_EXEC:
            return f"{host}#"
        if self.mode == GLOBAL_CONFIG:
            return f"{host}(config)#"
        if self.mode == INTERFACE_CONFIG:
            return f"{host}(config-if)#"
        return f"{host}?>"

    def run(self):
        # (Unchanged)
        print("--- Basic Cisco CLI Simulator (Tab Completion & Abbreviation Enabled) ---")
        print("Type '?' for help, 'exit' or 'quit' to leave.")
        while True:
            try:
                prompt = self.get_prompt()
                line = input(prompt).strip()
                if line:
                    # Add non-empty, non-history commands to readline history
                    if READLINE_AVAILABLE and line.lower() != 'history' and (not self.command_history or line != self.command_history[-1]):
                        readline.add_history(line)
                    # Keep our internal history too
                    self.command_history.append(line)
                    self.process_command(line)
            except (EOFError, KeyboardInterrupt):
                print("\nExiting simulator.")
                break
            except AmbiguousCommandError as ace:
                print(f"% Ambiguous command: \"{ace}\"")
            except InvalidInputError as iie:
                self.print_invalid_input(line, str(iie))
            except Exception as e:
                print(f"An unexpected error occurred: {e}")
                import traceback
                traceback.print_exc()  # More debug info

    # --- NEW: Helper to get all valid commands for the current mode ---
    def _get_valid_commands_for_mode(self):
        """Returns a list of all valid command *starters* for the current mode."""
        commands = list(self.commands.get(self.mode, {}).keys())
        # Add commands handled specially or in sub-modes if applicable
        # Note: These are the *first words* of commands
        if self.mode == PRIV_EXEC:
            # 'show' and 'configure' are already in self.commands[PRIV_EXEC]
            pass
        if self.mode == GLOBAL_CONFIG:
            # 'hostname', 'interface', 'no' are already in self.commands[GLOBAL_CONFIG]
            pass
        if self.mode == INTERFACE_CONFIG:
            # 'ip', 'shutdown', 'no' are already in self.commands[INTERFACE_CONFIG]
            pass

        # Add base commands if applicable (like exit, end, ?)
        # These should already be in the dictionaries, but ensure consistency
        if '?' not in commands:
            commands.append('?')
        if self.mode != USER_EXEC and 'exit' not in commands:
            commands.append('exit')
        if self.mode in [GLOBAL_CONFIG, INTERFACE_CONFIG] and 'end' not in commands:
            commands.append('end')

        return sorted(list(set(commands)))  # Deduplicate and sort

    # --- NEW: Helper to find command by abbreviation ---
    def _find_command_by_abbreviation(self, user_input, available_commands):
        """
        Finds a unique command from available_commands that starts with user_input.
        Returns the full command name if unique.
        Raises AmbiguousCommandError if multiple matches.
        Raises InvalidInputError if no match.
        """
        user_input_lower = user_input.lower()
        matches = [cmd for cmd in available_commands if cmd.lower(
        ).startswith(user_input_lower)]

        if len(matches) == 1:
            return matches[0]
        elif len(matches) > 1:
            # Check for exact match among ambiguous options
            exact_match = [cmd for cmd in matches if cmd.lower()
                           == user_input_lower]
            if len(exact_match) == 1:
                return exact_match[0]
            raise AmbiguousCommandError(user_input)
        else:
            raise InvalidInputError(user_input)  # No command starts with this

    def process_command(self, line):
        """Parses and executes a command line using abbreviation."""
        parts = line.split()
        if not parts:
            return

        user_cmd_input = parts[0]
        args = parts[1:]

        # --- Find command handler using abbreviation ---
        available_commands = self._get_valid_commands_for_mode()
        try:
            full_command = self._find_command_by_abbreviation(
                user_cmd_input, available_commands)
        except (AmbiguousCommandError, InvalidInputError) as e:
            # Reraise specific errors to be caught in run() loop
            raise e

        # --- Get the handler ---
        handler = self.commands.get(self.mode, {}).get(full_command)

        # --- Execute ---
        if handler:
            try:
                # Pass the *original* arguments list to the handler
                handler(args)
            except TypeError as te:
                # Check arity mismatch more carefully
                sig = inspect.signature(handler)
                num_required_params = len([p for p in sig.parameters.values(
                    # Count non-default params excluding self
                ) if p.default is p.empty and p.name != 'self'])
                if len(args) < num_required_params:
                    print(f"% Incomplete command.")
                else:
                    # Could be too many args if handler takes none, or other type error
                    # Let specific handlers raise ValueError for bad args
                    print(
                        f"% Invalid input or arguments for command '{full_command}'.")
                    # print(f"Debug TypeError: {te}") # Uncomment for debug
            except ValueError as ve:  # Handlers should raise ValueError for bad args
                print(f"% {ve}")  # Print specific error from handler
            except IndexError:
                # Should be less common now, but catch just in case
                print("% Incomplete command.")
        else:
            # This case should be less likely if _get_valid_commands covers all handlers
            print(
                f"% Command lookup failed for '{full_command}' (internal error).")

    def print_invalid_input(self, line, command_part):
        """Prints the standard Cisco 'Invalid input' error."""
        # Try to find the specific part that caused the error
        marker_pos = -1
        # Use regex to find the first occurrence, case-insensitive
        try:
            # Escape regex special characters in the command part
            escaped_part = re.escape(command_part)
            match = re.search(escaped_part, line, re.IGNORECASE)
            if match:
                marker_pos = match.start()
        except re.error:
            pass  # Ignore regex errors if command_part is weird

        if marker_pos == -1:
            # Fallback: find first non-space character
            match = re.search(r'\S', line)
            marker_pos = match.start() if match else 0

        print("% Invalid input detected at '^' marker.")
        print(f"  {line}")
        print(f"  {' ' * marker_pos}^")

    # --- Command Handlers (do_* methods - updated for arg abbreviation) ---

    def do_help(self, args):
        # (Implementation largely unchanged)
        print("Available commands in this context:")
        # Use the helper to get relevant commands
        valid_commands = self._get_valid_commands_for_mode()
        for cmd in valid_commands:
            print(f"  {cmd}")
        print("(Use TAB for completion, abbreviations are supported)")

    def do_exit_quit(self, args):
        # (Unchanged)
        if self.mode == USER_EXEC or self.mode == PRIV_EXEC:
            print("Exiting simulator.")
            sys.exit(0)
        else:
            # Should not happen due to command availability check, but good practice
            raise ValueError("Command not available in this mode.")

    def do_exit_mode(self, args):
        # (Unchanged)
        if self.mode == GLOBAL_CONFIG:
            self.mode = PRIV_EXEC
        elif self.mode == INTERFACE_CONFIG:
            self.mode = GLOBAL_CONFIG
            self.current_interface = None  # Clear current interface when exiting if-mode

    def do_end(self, args):
        # (Unchanged)
        if self.mode in [GLOBAL_CONFIG, INTERFACE_CONFIG]:
            self.mode = PRIV_EXEC
            self.current_interface = None
        else:
            raise ValueError("Command not valid in this mode.")

    def do_enable(self, args):
        # (Unchanged)
        self.mode = PRIV_EXEC
        # Real Cisco might ask for password here if configured
        # print("% Password: ***** (simulated)") # Keep it simple

    def do_disable(self, args):
        # (Unchanged)
        self.mode = USER_EXEC

    def do_configure(self, args):
        """Enters Global Configuration mode. Expects arg starting with 't'."""
        # Check for abbreviation 't' for 'terminal'
        if not args or not args[0].lower().startswith('t'):
            # Raise ValueError for specific feedback
            raise ValueError(
                "Incomplete command. Expecting 'configure terminal'")
        self.mode = GLOBAL_CONFIG
        print("Enter configuration commands, one per line. End with CNTL/Z or 'end'.")

    def do_hostname(self, args):
        """Sets hostname. Expects [new_hostname] in args."""
        if not args:
            raise ValueError("Incomplete command. Expecting 'hostname <name>'")
        new_hostname = args[0]
        # Basic validation (unchanged)
        if not re.match(r"^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$", new_hostname):
            raise ValueError("Invalid hostname.")
        self.running_config['hostname'] = new_hostname

    def _normalize_interface_name(self, type_part, num_part):
        """Standardizes interface names like g -> GigabitEthernet."""
        t = type_part.lower()
        if t.startswith('g'):
            base = 'GigabitEthernet'
        elif t.startswith('f'):
            base = 'FastEthernet'
        elif t.startswith('e'):  # Less common but possible
            base = 'Ethernet'
        else:
            return None  # Invalid type
        # Basic validation for number part
        if not re.match(r"^\d+/\d+$", num_part):
            return None
        return f"{base}{num_part}"

    def do_interface(self, args):
        """Enters interface config mode. Expects [type, number] in args (abbreviations ok)."""
        if len(args) < 1:  # Need at least the type part
            raise ValueError(
                "Incomplete command. Expecting 'interface <type><number>' or 'interface <type> <number>'")

        # Join args in case user typed "g 0/0" vs "g0/0"
        intf_input = "".join(args)

        # Use regex to separate type abbreviation/name from number
        # Allows forms like: g0/0, gi0/0, GigabitEthernet0/0, f0/1, fa0/1
        match = re.match(r"([a-zA-Z]+)\s*(\d+/\d+)", intf_input, re.IGNORECASE)
        if not match:
            raise ValueError(
                f"Invalid interface format: {intf_input}. Expecting e.g., 'g0/0', 'FastEthernet0/1'")

        intf_type_part = match.group(1)
        intf_num_part = match.group(2)

        intf_name = self._normalize_interface_name(
            intf_type_part, intf_num_part)

        if not intf_name:
            raise ValueError(
                f"Invalid interface type or number: {intf_type_part} {intf_num_part}")

        # Create interface entry if it doesn't exist
        if intf_name not in self.running_config['interfaces']:
            self.running_config['interfaces'][intf_name] = {
                'ip_address': None, 'subnet_mask': None,
                'status': 'administratively down',  # Initial state
                'admin_status': 'down'  # Explicit admin state
            }
        self.current_interface = intf_name
        self.mode = INTERFACE_CONFIG

    def do_ip(self, args):
        """Handles IP commands. Expects arg starting with 'a', then ip, subnet."""
        if not self.current_interface:
            raise ValueError(
                "Command must be run in interface configuration mode.")
        # Check for 'address' abbreviation
        if not args or not args[0].lower().startswith('a'):
            raise ValueError(
                "Incomplete command. Expecting 'ip address <ip> <subnet>'")
        if len(args) != 3:
            raise ValueError(
                "Incorrect number of arguments. Expecting 'ip address <ip> <subnet>'")

        ip_addr, subnet_mask = args[1], args[2]
        if not self._is_valid_ip(ip_addr):
            raise ValueError(f"Invalid IP address format: {ip_addr}")
        if not self._is_valid_ip(subnet_mask):
            raise ValueError(f"Invalid subnet mask format: {subnet_mask}")

        intf_data = self.running_config['interfaces'][self.current_interface]
        intf_data.update({'ip_address': ip_addr, 'subnet_mask': subnet_mask})
        # Update status only if admin status is up
        if intf_data['admin_status'] == 'up':
            # Interface comes up when IP is assigned (if not admin down)
            intf_data['status'] = 'up'

    def _is_valid_ip(self, ip_str):
        # (Unchanged)
        parts = ip_str.split('.')
        if len(parts) != 4:
            return False
        try:
            return all(0 <= int(p) <= 255 for p in parts)
        except ValueError:
            return False

    def do_shutdown(self, args):
        """Administratively disables the current interface. Expects empty args."""
        if not self.current_interface:
            raise ValueError(
                "Command must be run in interface configuration mode.")
        if args:
            raise ValueError("Command 'shutdown' takes no arguments.")

        intf_data = self.running_config['interfaces'][self.current_interface]
        intf_data['status'] = 'administratively down'
        intf_data['admin_status'] = 'down'

    def do_no(self, args):
        """Handles 'no' commands. Expects [sub_command, ...] in args (abbreviations ok)."""
        if not args:
            raise ValueError("Incomplete 'no' command.")

        # Find the sub-command using abbreviation relative to the current mode's 'no' options
        no_sub_command_input = args[0]
        sub_args = args[1:]

        possible_no_commands = []
        if self.mode == INTERFACE_CONFIG:
            # What can follow 'no' in IF config
            possible_no_commands = ['shutdown', 'ip']
        elif self.mode == GLOBAL_CONFIG:
            # What can follow 'no' in GLOBAL config
            possible_no_commands = ['hostname', 'interface']

        # Find the specific 'no' sub-command (e.g., 'shutdown' or 'ip')
        matched_sub_command = None
        ambiguous = False
        matches = [cmd for cmd in possible_no_commands if cmd.startswith(
            no_sub_command_input.lower())]

        if len(matches) == 1:
            matched_sub_command = matches[0]
        elif len(matches) > 1:
            # Check for exact match among ambiguous options
            exact_match = [cmd for cmd in matches if cmd.lower()
                           == no_sub_command_input.lower()]
            if len(exact_match) == 1:
                matched_sub_command = exact_match[0]
            else:
                ambiguous = True

        if ambiguous:
            raise AmbiguousCommandError(f"no {no_sub_command_input}")
        if not matched_sub_command:
            # Unrecognized 'no' variant
            raise InvalidInputError(f"no {no_sub_command_input}")

        # --- Execute the specific 'no' action ---
        if self.mode == INTERFACE_CONFIG:
            if matched_sub_command == 'shutdown':
                self._no_shutdown(sub_args)
            elif matched_sub_command == 'ip':
                # Further check for 'address' abbreviation
                if not sub_args or not sub_args[0].lower().startswith('a'):
                    raise ValueError(
                        "Incomplete command. Expecting 'no ip address'")
                # Pass args after 'address' (should be none)
                self._no_ip_address(sub_args[1:])
            # Add other 'no' interface commands here if needed
        elif self.mode == GLOBAL_CONFIG:
            if matched_sub_command == 'hostname':
                raise ValueError(
                    "Cannot 'no hostname'. Set a new one instead.")
            elif matched_sub_command == 'interface':
                raise ValueError(
                    "Use 'default interface <name>' to reset an interface (not implemented).")
            # Add other 'no' global commands here if needed
        else:
            # Should not be reachable if mode checks work
            raise ValueError("'no' command not applicable in this mode.")

    def _no_shutdown(self, args):
        """Handles 'no shutdown'."""
        if not self.current_interface:
            return  # Should be caught earlier
        if args:
            raise ValueError("'no shutdown' takes no arguments.")

        intf_data = self.running_config['interfaces'][self.current_interface]
        intf_data['admin_status'] = 'up'
        # Interface only comes up if it has an IP or is configured for DHCP etc.
        # Simple simulation: comes up if IP exists, otherwise stays down (protocol down)
        intf_data['status'] = 'up' if intf_data.get('ip_address') else 'down'

    def _no_ip_address(self, args):
        """Handles 'no ip address'."""
        if not self.current_interface:
            return  # Should be caught earlier
        if args:
            raise ValueError("'no ip address' takes no further arguments.")

        intf_data = self.running_config['interfaces'][self.current_interface]
        intf_data.update({'ip_address': None, 'subnet_mask': None})
        # If admin status is up, the operational status goes down without an IP
        if intf_data['admin_status'] == 'up':
            intf_data['status'] = 'down'

    # --- Show Commands ---
    def do_show(self, args):
        """Handles 'show' commands. Expects [sub_command, ...] in args (abbreviations ok)."""
        if not args:
            raise ValueError(
                "Incomplete command. Expecting 'show <subcommand>'")

        sub_command_input = args[0]
        sub_args = args[1:]

        # Define possible 'show' subcommands
        show_options = ['version', 'running-config', 'run', 'ip', 'history']

        # Find subcommand by abbreviation
        matches = [opt for opt in show_options if opt.startswith(
            sub_command_input.lower())]
        matched_sub_command = None
        ambiguous = False

        if len(matches) == 1:
            matched_sub_command = matches[0]
        elif len(matches) > 1:
            # Special case: 'run' is abbreviation for 'running-config'
            if sub_command_input.lower() == 'run' and 'running-config' in matches:
                matched_sub_command = 'running-config'
            # Check for exact match
            elif sub_command_input.lower() in matches:
                matched_sub_command = sub_command_input.lower()
            else:
                # Handle ambiguity between 'run'/'running-config' if user types 'r'
                if sub_command_input.lower() == 'r' and 'run' in matches and 'running-config' in matches:
                    ambiguous = True
                # Add other ambiguity checks if needed
                else:
                    ambiguous = True

        if ambiguous:
            raise AmbiguousCommandError(f"show {sub_command_input}")
        if not matched_sub_command:
            # Unrecognized 'show' variant
            raise InvalidInputError(f"show {sub_command_input}")

        # --- Execute specific show command ---
        if matched_sub_command == 'version':
            self.show_version(sub_args)
        elif matched_sub_command == 'running-config':  # Handles 'run' as well now
            self.show_running_config(sub_args)
        elif matched_sub_command == 'history':
            # Pass args, though show_history ignores them
            self.show_history(sub_args)
        elif matched_sub_command == 'ip':
            # Handle 'show ip ...' subcommands
            if not sub_args:
                raise ValueError(
                    "Incomplete command. Expecting 'show ip <subcommand>'")
            ip_sub_command_input = sub_args[0]
            ip_sub_args = sub_args[1:]
            # Define 'show ip' options
            show_ip_options = ['interface']
            ip_matches = [opt for opt in show_ip_options if opt.startswith(
                ip_sub_command_input.lower())]

            if len(ip_matches) == 1:
                matched_ip_sub = ip_matches[0]
                if matched_ip_sub == 'interface':
                    # Handle 'show ip interface ...'
                    if not ip_sub_args:
                        raise ValueError(
                            "Incomplete command. Expecting 'show ip interface <subcommand>' or 'brief'")
                    ip_int_sub_input = ip_sub_args[0]
                    # Define 'show ip interface' options
                    # Add specific interfaces later?
                    show_ip_int_options = ['brief']
                    ip_int_matches = [opt for opt in show_ip_int_options if opt.startswith(
                        ip_int_sub_input.lower())]

                    if len(ip_int_matches) == 1:
                        matched_ip_int_sub = ip_int_matches[0]
                        if matched_ip_int_sub == 'brief':
                            self.show_ip_interface_brief(
                                ip_sub_args[1:])  # Pass args after 'brief'
                        else:
                            raise InvalidInputError(
                                f"show ip interface {ip_int_sub_input}")
                    elif len(ip_int_matches) > 1:
                        raise AmbiguousCommandError(
                            f"show ip interface {ip_int_sub_input}")
                    else:
                        raise InvalidInputError(
                            f"show ip interface {ip_int_sub_input}")
                else:
                    # Other 'show ip' commands if added
                    raise InvalidInputError(f"show ip {ip_sub_command_input}")
            elif len(ip_matches) > 1:
                raise AmbiguousCommandError(f"show ip {ip_sub_command_input}")
            else:
                raise InvalidInputError(f"show ip {ip_sub_command_input}")
        else:
            # Should not be reached
            print(
                f"% Internal error processing show command: {matched_sub_command}")

    # --- show_version, show_running_config, _sort_interface_key, show_ip_interface_brief, show_history methods ---
    # (Largely unchanged, but ensure they handle potential extra args gracefully if needed)

    def show_version(self, args):
        if args:
            print(f"% Invalid input detected near '{args[0]}'")  # Basic check
        print("Basic Cisco IOS Simulator (Python)\nVersion: 1.2 (Simulated w/ Abbreviation)\nHostname: {}\nUptime: {} (Simulated)".format(
            self.running_config['hostname'], time.strftime('%Hh %Mm %Ss', time.gmtime(time.time()))))

    def show_running_config(self, args):
        if args:
            print(f"% Invalid input detected near '{args[0]}'")
        print("Building configuration...")
        print("Current configuration:")  # More realistic start
        print("!")
        print(f"version 1.2 (Simulated)")
        print("!")
        print(f"hostname {self.running_config['hostname']}")
        print("!")
        # Ensure interfaces are sorted correctly
        sorted_interfaces = sorted(
            self.running_config['interfaces'].keys(), key=self._sort_interface_key)
        for intf_name in sorted_interfaces:
            intf_data = self.running_config['interfaces'][intf_name]
            print(f"interface {intf_name}")
            if intf_data.get('ip_address'):
                print(
                    f" ip address {intf_data['ip_address']} {intf_data['subnet_mask']}")
            # Only show shutdown if it's administratively down
            if intf_data.get('admin_status') == 'down':
                print(" shutdown")
            print("!")
        print("!")
        print("end")

    def _sort_interface_key(self, intf_name):
        # Match common interface types and numbers
        match = re.match(r"([a-zA-Z]+)(\d+)/(\d+)", intf_name)
        if match:
            type_prefix = match.group(1).lower()
            # Assign weights for sorting order (Ethernet < FastEth < GigEth < TenGig etc.)
            type_weight = 99  # Default for unknown
            if type_prefix.startswith('e'):
                type_weight = 1
            if type_prefix.startswith('f'):
                type_weight = 2
            if type_prefix.startswith('g'):
                type_weight = 3
            if type_prefix.startswith('t'):
                type_weight = 4  # TenGigabitEthernet

            slot = int(match.group(2))
            port = int(match.group(3))
            return (type_weight, slot, port)
        # Fallback for non-matching names (shouldn't happen with normalization)
        return (999, 0, 0)

    def show_ip_interface_brief(self, args):
        if args:
            print(f"% Invalid input detected near '{args[0]}'")
        print("Interface                  IP-Address      OK? Method Status                Protocol")
        sorted_interfaces = sorted(
            self.running_config['interfaces'].keys(), key=self._sort_interface_key)
        if not sorted_interfaces:
            print("% No interfaces configured for IP.")  # Message if empty
            return
        for intf_name in sorted_interfaces:
            intf_data = self.running_config['interfaces'][intf_name]
            ip_addr = intf_data.get('ip_address', 'unassigned')
            # OK? is YES if IP is assigned AND interface is admin up
            ok = "YES" if ip_addr != 'unassigned' and intf_data.get(
                'admin_status') == 'up' else "NO"
            method = "manual" if ip_addr != 'unassigned' else "unset"
            # Status reflects admin status first
            status = intf_data.get('status', 'down')
            # Protocol is 'up' only if status is 'up' (simple simulation)
            protocol = 'up' if status == 'up' else 'down'
            print(
                f"{intf_name:<26} {ip_addr:<15} {ok:<3} {method:<6} {status:<21} {protocol}")

    def show_history(self, args=None):  # Accept args but ignore them
        if args:
            print(f"% Invalid input detected near '{args[0]}'")
        # Use readline's history if available for more realistic behavior
        if READLINE_AVAILABLE:
            history_len = readline.get_current_history_length()
            if history_len <= 0:
                print("Command history is empty.")
                return
            print("Command History (from readline):")
            # Readline history is 1-based index
            for i in range(1, history_len + 1):
                print(f" {i}: {readline.get_history_item(i)}")
        else:
            # Fallback to internal list
            if not self.command_history:
                print("Command history is empty.")
                return
            print("Command History (internal):")
            for i, cmd in enumerate(self.command_history):
                print(f" {i+1}: {cmd}")


# --- Main Execution ---
if __name__ == "__main__":
    simulator = CiscoDeviceSimulator()
    simulator.run()
