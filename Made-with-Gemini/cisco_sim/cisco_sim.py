#!/usr/bin/env python3

import sys
import time
import re

# --- Attempt to use readline ---
# This should now succeed in your WSL environment if installed correctly
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

# --- Tab Completion Logic ---

class CiscoCompleter:
    """Handles command completion based on simulator state."""
    def __init__(self, simulator):
        self.simulator = simulator
        self.matches = []

    def _get_available_commands(self):
        """Get commands relevant to the current mode."""
        mode = self.simulator.mode
        commands = list(self.simulator.commands.get(mode, {}).keys())

        # Add context-specific commands not directly in the dict keys
        if mode == PRIV_EXEC:
            commands.append('show')
            commands.append('configure') # Often completed from 'conf'
        if mode in [GLOBAL_CONFIG, INTERFACE_CONFIG]:
            commands.append('no')
        if mode == GLOBAL_CONFIG:
             commands.append('interface')
             commands.append('hostname')
        if mode == INTERFACE_CONFIG:
             commands.append('ip')
             commands.append('shutdown')


        # Add base commands if applicable (like exit, end)
        if 'exit' not in commands and mode != USER_EXEC:
             commands.append('exit')
        if 'end' not in commands and mode in [GLOBAL_CONFIG, INTERFACE_CONFIG]:
             commands.append('end')

        return sorted(list(set(commands))) # Deduplicate and sort

    def _get_show_completions(self, line_parts):
        """Completions for 'show ...'"""
        show_options = ['version', 'running-config', 'run', 'ip', 'history']
        if len(line_parts) == 2: # Completing 'show <word>'
             current_text = line_parts[1]
             return [s + ' ' for s in show_options if s.startswith(current_text)]
        elif len(line_parts) == 3 and line_parts[1] == 'ip': # Completing 'show ip <word>'
             current_text = line_parts[2]
             ip_options = ['interface']
             return [s + ' ' for s in ip_options if s.startswith(current_text)]
        elif len(line_parts) == 4 and line_parts[1] == 'ip' and line_parts[2] == 'interface': # Completing 'show ip interface <word>'
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
             return [s + (' ' if s in types else '/') for s in possible if s.startswith(current_text)] # Add / hint for types
         elif len(line_parts) > 2: # Potentially completing number part
             # Basic completion for number part if type already typed
             # More complex logic needed for full Gi0/0 style completion split
             intf_type = line_parts[1]
             current_text = line_parts[-1] # Text being completed
             # Check if previous part looks like a type
             if intf_type.lower().startswith(('g','f')):
                  # Suggest common numbers or complete existing full names
                  base_name = "".join(line_parts[1:-1]) # Reconstruct potential base name
                  existing = list(self.simulator.running_config['interfaces'].keys())
                  completions = []
                  # Suggest simple numbers
                  for i in range(5): # Suggest 0/0 to 0/4
                       num = f"0/{i}"
                       if num.startswith(current_text):
                            completions.append(num + ' ')
                  # Suggest completions for existing interfaces if matching type
                  for name in existing:
                       if name.lower().startswith(intf_type.lower()) and name.startswith(base_name + current_text):
                            completions.append(name[len(base_name):] + ' ') # Complete the rest
                  return list(set(completions)) # Unique suggestions

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
              elif len(line_parts) == 3 and line_parts[1] == 'ip':
                  current_text = line_parts[2]
                  ip_options = ['address']
                  return [s + ' ' for s in ip_options if s.startswith(current_text)]
         # Add 'no' commands for GLOBAL_CONFIG if any are implemented
         return []


    def complete(self, text, state):
        """This is the completer function called by readline."""
        line = readline.get_line_buffer()
        line_parts = line.split()

        # If the line is empty or the cursor is at the beginning of a word
        if not line or line.endswith(' '):
             current_text = ""
        else:
             current_text = line_parts[-1]

        if state == 0:
            # This is the first time for this completion, compute matches
            self.matches = []

            # --- Context-Specific Completion ---
            if len(line_parts) == 0 or (len(line_parts) == 1 and not line.endswith(' ')):
                # Completing the very first command word
                self.matches = [cmd + ' ' for cmd in self._get_available_commands() if cmd.startswith(current_text)]

            elif line_parts[0] == 'show' and self.simulator.mode == PRIV_EXEC:
                 self.matches = self._get_show_completions(line_parts)

            elif line_parts[0] == 'configure' and self.simulator.mode == PRIV_EXEC:
                 if len(line_parts) <= 2 and not line.endswith(' '):
                     options = ['terminal']
                     self.matches = [opt + ' ' for opt in options if opt.startswith(line_parts[-1])]

            elif line_parts[0] == 'interface' and self.simulator.mode == GLOBAL_CONFIG:
                 self.matches = self._get_interface_completions(line_parts)

            elif line_parts[0] == 'hostname' and self.simulator.mode == GLOBAL_CONFIG:
                 pass # No standard completions for hostname value

            elif line_parts[0] == 'ip' and self.simulator.mode == INTERFACE_CONFIG:
                 self.matches = self._get_ip_completions(line_parts)

            elif line_parts[0] == 'no':
                 self.matches = self._get_no_completions(line_parts)

            # Add more context specific completions here (e.g., for 'no', specific args)

            # --- Generic Completion (Fallback) ---
            # If no specific context matched or cursor is at start of word, offer base commands
            if not self.matches and (len(line_parts) <= 1 or line.endswith(' ')):
                 self.matches = [cmd + ' ' for cmd in self._get_available_commands() if cmd.startswith(current_text)]


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
            'interfaces': {}
        }
        self.current_interface = None
        self.command_history = []

        # Command Definitions (unchanged)
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
                'end': self.do_end,
                # 'configure': self.do_configure, # Handled via completion/parsing
                # 'show': self.do_show,           # Handled via completion/parsing
                '?': self.do_help,
            },
            GLOBAL_CONFIG: {
                'exit': self.do_exit_mode,
                'end': self.do_end,
                # 'hostname': self.do_hostname, # Handled via completion/parsing
                # 'interface': self.do_interface,# Handled via completion/parsing
                # 'no': self.do_no,             # Handled via completion/parsing
                '?': self.do_help,
            },
            INTERFACE_CONFIG: {
                'exit': self.do_exit_mode,
                'end': self.do_end,
                # 'ip': self.do_ip,             # Handled via completion/parsing
                # 'shutdown': self.do_shutdown, # Handled via completion/parsing
                # 'no': self.do_no,             # Handled via completion/parsing
                '?': self.do_help,
            }
        }

        # --- Setup Readline Completion (if available) ---
        if READLINE_AVAILABLE:
            self.completer = CiscoCompleter(self)
            readline.set_completer(self.completer.complete)
            # Use common delimiters: space, tab, newline, etc.
            readline.set_completer_delims(' \t\n;')
            # Bind Tab key to completion function
            readline.parse_and_bind('tab: complete')

    # --- get_prompt, run, process_command methods (mostly unchanged) ---
    def get_prompt(self):
        host = self.running_config['hostname']
        if self.mode == USER_EXEC: return f"{host}>"
        if self.mode == PRIV_EXEC: return f"{host}#"
        if self.mode == GLOBAL_CONFIG: return f"{host}(config)#"
        if self.mode == INTERFACE_CONFIG: return f"{host}(config-if)#"
        return f"{host}?>"

    def run(self):
        print("--- Basic Cisco CLI Simulator (Tab Completion Enabled) ---")
        print("Type '?' for help, 'exit' or 'quit' to leave.")
        while True:
            try:
                prompt = self.get_prompt()
                line = input(prompt).strip()
                if line:
                    self.command_history.append(line)
                    self.process_command(line)
            except (EOFError, KeyboardInterrupt):
                print("\nExiting simulator.")
                break
            except Exception as e:
                print(f"An unexpected error occurred: {e}")

    def process_command(self, line):
        """Parses and executes a command line."""
        parts = line.split()
        if not parts: return

        command = parts[0].lower()
        args = parts[1:]

        # --- Find command handler (adjusted for multi-word commands) ---
        handler = None
        # Prioritize specific handlers from the dictionary
        if command in self.commands.get(self.mode, {}):
             handler = self.commands[self.mode][command]

        # Handle multi-word commands/subcommands if no direct handler found
        # Note: The actual logic is now more distributed into do_* methods
        elif self.mode == PRIV_EXEC:
            if command == 'show': handler = self.do_show
            elif command == 'configure': handler = self.do_configure
        elif self.mode == GLOBAL_CONFIG:
            if command == 'hostname': handler = self.do_hostname
            elif command == 'interface': handler = self.do_interface
            elif command == 'no': handler = self.do_no
        elif self.mode == INTERFACE_CONFIG:
            if command == 'ip': handler = self.do_ip
            elif command == 'shutdown': handler = self.do_shutdown
            elif command == 'no': handler = self.do_no
        elif command == '?': # Allow '?' even if not explicitly in self.commands[mode]
             handler = self.do_help

        # --- Execute ---
        if handler:
            try:
                handler(args) # Pass the rest of the line as args
            except TypeError as te:
                 # Check if it's an arity mismatch (e.g., handler expected args)
                 import inspect
                 try:
                     sig = inspect.signature(handler)
                     # Simple check: if it needs more than just 'self' (implicitly passed for methods)
                     if len(sig.parameters) > 0:
                          print(f"% Incomplete command.") # Or more specific error
                     else: # Handler takes no args, but some were given?
                          print(f"% Extra arguments provided for '{command}'.")
                 except ValueError: # Cannot inspect built-ins easily sometimes
                      print(f"% Incomplete command or argument error for '{command}'.")
                 # print(f"Debug TypeError: {te}") # Uncomment for debug
            except ValueError as ve:
                 print(f"% Error: {ve}")
            except IndexError:
                 # Often happens if args list is shorter than expected
                 print("% Incomplete command.")
        else:
            self.print_invalid_input(line, command)


    def print_invalid_input(self, line, command):
        """Prints the standard Cisco 'Invalid input' error."""
        # Find where the command actually starts in the original line
        # Use regex to handle potential leading spaces if input() didn't strip perfectly
        match = re.search(re.escape(command), line, re.IGNORECASE)
        marker_pos = match.start() if match else line.find(command) # Fallback
        if marker_pos == -1: marker_pos = 0
        print("% Invalid input detected at '^' marker.")
        print(f"  {line}")
        print(f"  {' ' * marker_pos}^")


    # --- Command Handlers (do_* methods - largely unchanged, ensure they handle args list) ---
    # Make sure methods like do_configure, do_interface, do_ip, do_no etc.
    # correctly parse the 'args' list they receive from process_command.
    # Example adjustments shown for a few:

    def do_help(self, args):
        # (Implementation largely unchanged, but could use completer logic too)
        print("Available commands in this context (use TAB for completion):")
        # Use completer's logic to get relevant commands
        completer_commands = self.completer._get_available_commands()
        for cmd in completer_commands:
             print(f"  {cmd}")
        # Add more specific help based on args if desired

    def do_exit_quit(self, args):
        if self.mode == USER_EXEC or self.mode == PRIV_EXEC:
            print("Exiting simulator.")
            sys.exit(0)
        else:
             print("% Command not available in this mode.")

    def do_exit_mode(self, args):
        if self.mode == GLOBAL_CONFIG: self.mode = PRIV_EXEC
        elif self.mode == INTERFACE_CONFIG: self.mode = GLOBAL_CONFIG
        self.current_interface = None

    def do_end(self, args):
         if self.mode in [GLOBAL_CONFIG, INTERFACE_CONFIG]:
              self.mode = PRIV_EXEC
              self.current_interface = None
         else: print("% Command not valid in this mode.")

    def do_enable(self, args):
        self.mode = PRIV_EXEC
        print("% Password: ***** (simulated)")

    def do_disable(self, args):
        self.mode = USER_EXEC

    def do_configure(self, args):
        """Enters Global Configuration mode. Expects ['terminal'] in args."""
        if not args or args[0].lower() != 'terminal':
            print("% Incomplete command. Expecting 'configure terminal'")
            return
        self.mode = GLOBAL_CONFIG
        print("Enter configuration commands, one per line. End with CNTL/Z or 'end'.")

    def do_hostname(self, args):
        """Sets hostname. Expects [new_hostname] in args."""
        if not args:
            print("% Incomplete command.")
            return
        new_hostname = args[0]
        # Basic validation
        if not re.match(r"^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$", new_hostname):
             print("% Invalid hostname.")
             return
        self.running_config['hostname'] = new_hostname

    def do_interface(self, args):
        """Enters interface config mode. Expects [type, number] in args."""
        if len(args) < 2:
            print("% Incomplete command. Expecting 'interface <type> <number>'")
            return
        intf_type = args[0] # Case might matter for display consistency
        intf_num = args[1]
        intf_name = f"{intf_type.capitalize().replace('ernet','Ethernet')}{intf_num}" # Improve standardization

        if not (intf_type.lower().startswith(('g','f','e'))) or '/' not in intf_num:
             print(f"% Invalid interface type or number: {intf_type} {intf_num}")
             return

        if intf_name not in self.running_config['interfaces']:
            self.running_config['interfaces'][intf_name] = {
                'ip_address': None, 'subnet_mask': None,
                'status': 'administratively down', 'admin_status': 'down'
            }
        self.current_interface = intf_name
        self.mode = INTERFACE_CONFIG


    def do_ip(self, args):
        """Handles IP commands. Expects ['address', ip, subnet] in args."""
        if not self.current_interface:
             print("% Command must be run in interface configuration mode.")
             return
        if not args or args[0].lower() != 'address':
             print("% Incomplete command. Expecting 'ip address <ip> <subnet>'")
             return
        if len(args) != 3:
            print("% Incorrect number of arguments. Expecting 'ip address <ip> <subnet>'")
            return
        ip_addr, subnet_mask = args[1], args[2]
        if not self._is_valid_ip(ip_addr) or not self._is_valid_ip(subnet_mask):
             print("% Invalid IP address or subnet mask.")
             return
        intf_data = self.running_config['interfaces'][self.current_interface]
        intf_data.update({'ip_address': ip_addr, 'subnet_mask': subnet_mask})
        if intf_data['admin_status'] == 'up': intf_data['status'] = 'up'

    def _is_valid_ip(self, ip_str):
        # (Unchanged)
        parts = ip_str.split('.')
        if len(parts) != 4: return False
        try: return all(0 <= int(p) <= 255 for p in parts)
        except ValueError: return False

    def do_shutdown(self, args):
        """Administratively disables the current interface. Expects empty args."""
        if not self.current_interface: return
        intf_data = self.running_config['interfaces'][self.current_interface]
        intf_data['status'] = 'administratively down'
        intf_data['admin_status'] = 'down'

    def do_no(self, args):
        """Handles 'no' commands. Expects [sub_command, ...] in args."""
        if not args:
            print("% Incomplete 'no' command.")
            return
        sub_command = args[0].lower()
        sub_args = args[1:]

        if self.mode == INTERFACE_CONFIG:
            if sub_command == 'shutdown': self._no_shutdown(sub_args)
            elif sub_command == 'ip' and sub_args and sub_args[0].lower() == 'address':
                 self._no_ip_address(sub_args[1:])
            else: print(f"% Unrecognized 'no' command variant: no {sub_command}")
        elif self.mode == GLOBAL_CONFIG:
             if sub_command == 'hostname': print("% Cannot 'no hostname'. Set a new one.")
             else: print(f"% Unrecognized 'no' command variant: no {sub_command}")
        else: print("% 'no' command not applicable in this mode.")

    def _no_shutdown(self, args):
        if not self.current_interface: return
        intf_data = self.running_config['interfaces'][self.current_interface]
        intf_data['admin_status'] = 'up'
        intf_data['status'] = 'up' if intf_data['ip_address'] else 'down'

    def _no_ip_address(self, args):
        if not self.current_interface: return
        intf_data = self.running_config['interfaces'][self.current_interface]
        intf_data.update({'ip_address': None, 'subnet_mask': None})
        if intf_data['admin_status'] == 'up': intf_data['status'] = 'down'

    # --- Show Commands ---
    def do_show(self, args):
        """Handles 'show' commands. Expects [sub_command, ...] in args."""
        if not args:
            print("% Incomplete command. Expecting 'show <subcommand>'")
            return
        sub_command = args[0].lower()
        sub_args = args[1:]

        if sub_command == 'version': self.show_version(sub_args)
        elif sub_command in ['running-config', 'run']: self.show_running_config(sub_args)
        elif sub_command == 'ip' and sub_args and sub_args[0].lower() == 'interface' and len(sub_args)>1 and sub_args[1].lower() == 'brief':
             self.show_ip_interface_brief(sub_args[2:])
        elif sub_command == 'history': self.show_history()
        else: print(f"% Unrecognized 'show' command variant: show {sub_command}")

    # --- show_version, show_running_config, _sort_interface_key, show_ip_interface_brief, show_history methods (Unchanged) ---
    def show_version(self, args): print("Basic Cisco IOS Simulator (Python)\nVersion: 1.1 (Simulated w/ Tab Completion)\nHostname: {}\nUptime: {} (Simulated)".format(self.running_config['hostname'], time.strftime('%Hh %Mm %Ss', time.gmtime(time.time()))))
    def show_running_config(self, args):
        print("Building configuration...")
        print("!\nversion 1.1 (Simulated)\n!")
        print(f"hostname {self.running_config['hostname']}\n!")
        sorted_interfaces = sorted(self.running_config['interfaces'].keys(), key=self._sort_interface_key)
        for intf_name in sorted_interfaces:
            intf_data = self.running_config['interfaces'][intf_name]
            print(f"interface {intf_name}")
            if intf_data.get('ip_address'): print(f" ip address {intf_data['ip_address']} {intf_data['subnet_mask']}")
            if intf_data.get('admin_status') == 'down': print(" shutdown")
            print("!")
        print("!\nend")
    def _sort_interface_key(self, intf_name):
        match = re.match(r"([a-zA-Z]+)(\d+)/(\d+)", intf_name)
        if match:
            type_prefix = match.group(1).lower()
            type_weight = {'fa': 1, 'gi': 2, 'te': 3}.get(type_prefix[:2], 99)
            slot, port = int(match.group(2)), int(match.group(3))
            return (type_weight, slot, port)
        return (999, 0, 0)
    def show_ip_interface_brief(self, args):
         print("Interface                  IP-Address      OK? Method Status                Protocol")
         sorted_interfaces = sorted(self.running_config['interfaces'].keys(), key=self._sort_interface_key)
         for intf_name in sorted_interfaces:
             intf_data = self.running_config['interfaces'][intf_name]
             ip_addr = intf_data.get('ip_address', 'unassigned')
             ok = "YES" if ip_addr != 'unassigned' else "NO"
             method = "manual" if ip_addr != 'unassigned' else "unset"
             status = intf_data.get('status', 'down')
             protocol = status # Simple simulation
             print(f"{intf_name:<26} {ip_addr:<15} {ok:<3} {method:<6} {status:<21} {protocol}")
    def show_history(self):
         print("Command History:")
         for i, cmd in enumerate(self.command_history): print(f" {i+1}: {cmd}")


# --- Main Execution ---
if __name__ == "__main__":
    simulator = CiscoDeviceSimulator()
    simulator.run()