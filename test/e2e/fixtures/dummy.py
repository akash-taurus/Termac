#!/usr/bin/env python3
"""
Test fixture for Milestone 1 launcher verification.
Validates stdout, stderr, environment variables, arguments, and exit codes.
"""
import sys
import os
import time

def main():
    args = sys.argv[1:]

    # Handle sleep command for timeout/cancellation tests
    if len(args) >= 2 and args[0] == "--sleep":
        sleep_secs = float(args[1])
        time.sleep(sleep_secs)
        return

    # Handle deliberate exit code command
    if len(args) >= 2 and args[0] == "--exit-code":
        code = int(args[1])
        sys.stderr.write(f"EXITING_WITH_CODE_{code}\n")
        sys.stderr.flush()
        sys.exit(code)

    # Standard execution handshake
    print("DUMMY_PY_STDOUT: OK")

    # Check for stderr test flag
    if os.environ.get("TEST_TRIGGER_STDERR") == "1":
        sys.stderr.write("DUMMY_PY_STDERR: NOTICE\n")
        sys.stderr.flush()

    # Echo environment variable if present
    env_val = os.environ.get("PLUGIN_TEST_ENV")
    if env_val:
        print(f"ENV:{env_val}")

    # Echo received arguments
    if args:
        print(f"ARGS:{','.join(args)}")

    # Echo current working directory
    print(f"CWD:{os.getcwd()}")
    sys.stdout.flush()

if __name__ == "__main__":
    main()
