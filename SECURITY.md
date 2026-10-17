# Security Policy

## Reporting a vulnerability

Report security issues privately through GitHub's [private vulnerability reporting](https://github.com/wahyunoerr/go-boost/security/advisories/new) rather than opening a public issue.

Please include the go-boost version, the tool involved, and the smallest input that reproduces the problem.

## Supported versions

Fixes land on the latest minor release. Older minor versions are not patched.

| Version | Supported |
| :--- | :--- |
| 2.x | Yes |
| < 2.0 | No |

## What go-boost does on your machine

go-boost is a developer tool that runs locally and is driven by an AI agent through MCP. Two of its capabilities deserve attention before you grant an agent access to them.

**`diagnose_run` executes a command.** That is its purpose: it supervises your application and diagnoses the crash. The command comes from the agent, so an agent acting on untrusted input, such as the contents of a log file or a database row, could choose what runs. It is bounded by a timeout and terminated as a process group, but it is not sandboxed. Review what your MCP client allows before enabling it.

**`db_query` is read-only, enforced twice.** Statements are validated against an allowlist of leading verbs and a denylist of mutating keywords and functions, and the database is additionally opened in a read-only mode: `sqlite3 -readonly`, `psql` with `default_transaction_read_only=on`, and `mysql` with a read-only session. A statement that gets past validation still cannot write. Credentials are never returned in tool output and are passed to the client through the environment rather than the command line.

The remaining tools only read source files and run `go` subcommands with arguments that are rejected if they are shaped like flags.
