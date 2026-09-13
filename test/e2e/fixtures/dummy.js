#!/usr/bin/env node
/**
 * Test fixture for Milestone 1 launcher verification.
 * Validates stdout, stderr, environment variables, arguments, and exit codes.
 */
const process = require('process');

function main() {
    const args = process.argv.slice(2);

    // Handle sleep command for timeout/cancellation tests
    if (args.length >= 2 && args[0] === '--sleep') {
        const sleepMs = parseFloat(args[1]) * 1000;
        setTimeout(() => {
            process.exit(0);
        }, sleepMs);
        return;
    }

    // Handle deliberate exit code command
    if (args.length >= 2 && args[0] === '--exit-code') {
        const code = parseInt(args[1], 10);
        process.stderr.write(`EXITING_WITH_CODE_${code}\n`);
        process.exit(code);
    }

    // Standard execution handshake
    console.log("DUMMY_JS_STDOUT: OK");

    // Check for stderr test flag
    if (process.env.TEST_TRIGGER_STDERR === "1") {
        process.stderr.write("DUMMY_JS_STDERR: NOTICE\n");
    }

    // Echo environment variable if present
    const envVal = process.env.PLUGIN_TEST_ENV;
    if (envVal) {
        console.log(`ENV:${envVal}`);
    }

    // Echo received arguments
    if (args.length > 0) {
        console.log(`ARGS:${args.join(',')}`);
    }

    // Echo current working directory
    console.log(`CWD:${process.cwd()}`);
}

main();
