package cli

import (
	"errors"
	"fmt"
	"testing"
)

func TestExitCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"plain", errors.New("plain"), 1},
		{"meta", &MetaError{Err: errors.New("bad query")}, 2},
		{"db", &DBError{Err: errors.New("no conn")}, 3},
		{"wrapped meta", fmt.Errorf("validate: %w", &MetaError{Err: errors.New("x")}), 2},
		{"double wrapped db", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", &DBError{Err: errors.New("x")})), 3},
	}
	for _, c := range cases {
		if got := ExitCode(c.err); got != c.want {
			t.Errorf("%s: ExitCode = %d, want %d", c.name, got, c.want)
		}
	}
}
