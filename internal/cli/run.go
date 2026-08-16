package cli

import (
	"errors"
	"fmt"
)

// ExitCode maps errors to process exit codes (spec v2 §4):
// 2 = metadata/query definition invalid, 3 = database/runtime failure.
func ExitCode(err error) int {
	var metaErr *MetaError
	if errors.As(err, &metaErr) {
		return 2
	}
	var dbErr *DBError
	if errors.As(err, &dbErr) {
		return 3
	}
	return 1
}

type MetaError struct{ Err error }

func (e *MetaError) Error() string {
	if e.Err == nil {
		return "metadata error"
	}
	return e.Err.Error()
}
func (e *MetaError) Unwrap() error { return e.Err }

type DBError struct{ Err error }

func (e *DBError) Error() string {
	if e.Err == nil {
		return "database error"
	}
	return e.Err.Error()
}
func (e *DBError) Unwrap() error { return e.Err }

func usage() {
	fmt.Println(`plinth - AI-maintained SQL BFF for existing PostgreSQL

Usage:
  plinth validate [--dir DIR]            offline: load and check queries
  plinth test --query NAME [--dir DIR]   run one query against the database
  plinth semantics pull [--dir DIR]      refresh semantics snapshot
  plinth serve [--dir DIR] [--addr ADDR] start the HTTP BFF
  plinth status [--dir DIR]              query count + recent audit tail`)
}

// Run dispatches subcommands.
func Run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "validate":
		return runValidate(args[1:])
	case "test":
		return runTest(args[1:])
	case "pull":
		return runPull(args[1:])
	case "serve":
		return runServe(args[1:])
	case "status":
		return runStatus(args[1:])
	case "semantics":
		if len(args) > 1 && args[1] == "pull" {
			return runPull(args[2:])
		}
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}
