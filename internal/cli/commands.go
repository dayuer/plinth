package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	osexec "os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dayuer/plinth/internal/audit"
	"github.com/dayuer/plinth/internal/exec"
	"github.com/dayuer/plinth/internal/meta"
	"github.com/dayuer/plinth/internal/queryfile"
	"github.com/dayuer/plinth/internal/registry"
	"github.com/dayuer/plinth/internal/server"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newFlagSet makes a subcommand flag set with the shared --dir flag.
func newFlagSet(name string, args []string, extra func(fs *flag.FlagSet)) (*flag.FlagSet, *string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory")
	if extra != nil {
		extra(fs)
	}
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	return fs, dir, nil
}

// loadConfig reads DIR/plinth.yml; a load failure is metadata (exit 2).
func loadConfig(dir string) (*meta.Config, error) {
	cfg, err := meta.LoadConfig(filepath.Join(dir, "plinth.yml"))
	if err != nil {
		return nil, &MetaError{Err: err}
	}
	return cfg, nil
}

// printErrs reports every LoadDir problem — load checks are collect-all, so
// the operator sees the full list in one pass.
func printErrs(cmd string, errs []error) {
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, cmd+":", e)
	}
}

// runValidate is the offline gate: load every query, report all problems,
// then warn about snapshots pinned to outdated semantics (spec v2 §7).
func runValidate(args []string) error {
	_, dir, err := newFlagSet("validate", args, nil)
	if err != nil {
		return err
	}
	reg, errs := registry.LoadDir(*dir)
	printErrs("validate", errs)
	if len(errs) > 0 {
		return &MetaError{Err: fmt.Errorf("%d query problem(s)", len(errs))}
	}
	warnSemanticsDrift(*dir, reg)
	fmt.Printf("validate: ok (%d queries)\n", len(reg.Names()))
	return nil
}

// warnSemanticsDrift prints a warning for each query that pins a semantics
// snapshot other than the current one: its SQL was written against older
// dataset semantics and needs review. Warnings never fail validation — a
// human decides. No snapshot.txt yet means nothing to compare against.
func warnSemanticsDrift(dir string, reg *registry.Registry) {
	if reg == nil {
		return
	}
	cur, err := os.ReadFile(filepath.Join(dir, "semantics", "snapshot.txt"))
	if err != nil {
		return
	}
	current := strings.TrimSpace(string(cur))
	for _, name := range reg.Names() {
		q := reg.Get(name)
		if q.SemDataset != "" && q.SemSnapshot != current {
			fmt.Fprintf(os.Stderr,
				"warning: query %s pins snapshot %s, current is %s — review SQL against new semantics\n",
				name, q.SemSnapshot, current)
		}
	}
}

// runTest executes one query against the configured database with a row cap
// of 5 — a smoke tool for the agent loop, not a general client.
func runTest(args []string) error {
	var name, params string
	_, dir, err := newFlagSet("test", args, func(fs *flag.FlagSet) {
		fs.StringVar(&name, "query", "", "query name to test")
		fs.StringVar(&params, "param", "", "k=v pairs, comma separated (values retyped by declared param type)")
	})
	if err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("test: --query is required")
	}
	cfg, err := loadConfig(*dir)
	if err != nil {
		return err
	}
	reg, errs := registry.LoadDir(*dir)
	printErrs("test", errs)
	if len(errs) > 0 || reg == nil {
		return &MetaError{Err: fmt.Errorf("queries not loadable; run 'plinth validate'")}
	}
	q := reg.Get(name)
	if q == nil {
		return &MetaError{Err: fmt.Errorf("query %q not found", name)}
	}
	in, err := parseParams(q.Params, params)
	if err != nil {
		return &MetaError{Err: err}
	}
	pool, err := pgxpool.New(context.Background(), cfg.Database.URL)
	if err != nil {
		return &DBError{Err: err}
	}
	defer pool.Close()
	eng := &exec.Engine{
		Pool:           pool,
		DefaultTimeout: time.Duration(cfg.Engine.DefaultTimeoutMs) * time.Millisecond,
		MaxRows:        5,
	}
	res, err := eng.Run(context.Background(), q, in)
	if err != nil {
		return &DBError{Err: fmt.Errorf("query %q: %w", name, err)}
	}
	fmt.Printf("test %s: %d rows (capped at 5)\n", name, len(res.Rows))
	for i, row := range res.Rows {
		fmt.Printf("  row[%d] = %v\n", i, row)
	}
	return nil
}

// parseParams turns the --param "k=v,k2=v2" flag into typed values chosen
// by each declared param's type, so engine coercion accepts them. Unknown
// names are rejected: the declared contract is the whole surface.
func parseParams(ps []queryfile.Param, spec string) (map[string]any, error) {
	in := map[string]any{}
	if strings.TrimSpace(spec) == "" {
		return in, nil
	}
	declared := map[string]queryfile.Param{}
	for _, p := range ps {
		declared[p.Name] = p
	}
	for _, kv := range strings.Split(spec, ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, fmt.Errorf("bad --param %q: want k=v", kv)
		}
		k = strings.TrimSpace(k)
		p, known := declared[k]
		if !known {
			return nil, fmt.Errorf("unknown parameter %q", k)
		}
		val, err := retype(p.Type, strings.TrimSpace(v))
		if err != nil {
			return nil, err
		}
		in[k] = val
	}
	return in, nil
}

// retype converts one CLI string to the Go value coercion expects for typ.
func retype(typ, s string) (any, error) {
	switch typ {
	case "int":
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parameter value %q: want int", s)
		}
		return float64(n), nil // Coerce accepts integral float64 for int params
	case "float":
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("parameter value %q: want float", s)
		}
		return f, nil
	case "bool":
		b, err := strconv.ParseBool(s) // "true"/"1"/…
		if err != nil {
			return nil, fmt.Errorf("parameter value %q: want bool", s)
		}
		return b, nil
	default: // str
		return s, nil
	}
}

// runPull (aka `semantics pull`) refreshes the datasets description via the
// configured pull_command and records its content hash as the snapshot
// version queries pin against (spec v2 §7).
func runPull(args []string) error {
	_, dir, err := newFlagSet("pull", args, nil)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(*dir)
	if err != nil {
		return err
	}
	fields := strings.Fields(cfg.Semantics.PullCommand)
	if len(fields) == 0 {
		return &MetaError{Err: fmt.Errorf("semantics.pull_command not configured in plinth.yml")}
	}
	out, err := osexec.Command(fields[0], fields[1:]...).Output()
	if err != nil {
		var ee *osexec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return &DBError{Err: fmt.Errorf("pull_command failed: %v: %s", err, strings.TrimSpace(string(ee.Stderr)))}
		}
		return &DBError{Err: fmt.Errorf("pull_command failed: %w", err)}
	}
	semDir := filepath.Join(*dir, "semantics")
	if err := os.MkdirAll(semDir, 0o755); err != nil {
		return &DBError{Err: err}
	}
	if err := os.WriteFile(filepath.Join(semDir, "datasets.yml"), out, 0o644); err != nil {
		return &DBError{Err: err}
	}
	sum := sha256.Sum256(out)
	version := hex.EncodeToString(sum[:])[:12]
	if err := os.WriteFile(filepath.Join(semDir, "snapshot.txt"), []byte(version+"\n"), 0o644); err != nil {
		return &DBError{Err: err}
	}
	fmt.Printf("semantics: pulled %d bytes, snapshot version %s\n", len(out), version)
	fmt.Println("write this version into your queries' 'semantics:' header as you review them")
	return nil
}

// buildStack assembles everything serve serves with, in construction order:
// pool → audit writer → engine → server. Shared by runServe and the
// integration test so the tested order is the real one. shutdown releases
// the pool and the audit file; call it exactly once.
func buildStack(cfg *meta.Config, reg *registry.Registry) (*server.Server, func() error, error) {
	pool, err := pgxpool.New(context.Background(), cfg.Database.URL)
	if err != nil {
		return nil, nil, &DBError{Err: err}
	}
	aud, err := audit.Open(cfg.Audit.Path, cfg.Audit.MaskParams)
	if err != nil {
		pool.Close()
		return nil, nil, &DBError{Err: err}
	}
	// cfg defaults keep both nonzero (meta.LoadConfig), satisfying the
	// engine contract server.New delegates to its caller.
	eng := &exec.Engine{
		Pool:           pool,
		DefaultTimeout: time.Duration(cfg.Engine.DefaultTimeoutMs) * time.Millisecond,
		MaxRows:        cfg.Engine.MaxRows,
	}
	srv := server.New(reg, eng, cfg.Auth.Tokens, aud)
	return srv, func() error { pool.Close(); return aud.Close() }, nil
}

// runServe starts the HTTP BFF. Any query problem is fatal — never serve a
// partial registry. SIGHUP hot-reloads queries: a failed reload keeps the
// old registry and continues serving.
func runServe(args []string) error {
	var addr string
	_, dir, err := newFlagSet("serve", args, func(fs *flag.FlagSet) {
		fs.StringVar(&addr, "addr", ":8080", "listen address")
	})
	if err != nil {
		return err
	}
	cfg, err := loadConfig(*dir)
	if err != nil {
		return err
	}
	reg, errs := registry.LoadDir(*dir)
	printErrs("serve", errs)
	if len(errs) > 0 || reg == nil {
		return &MetaError{Err: fmt.Errorf("refusing to serve with invalid queries")}
	}
	srv, shutdown, err := buildStack(cfg, reg)
	if err != nil {
		return err
	}
	defer func() {
		if err := shutdown(); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			newReg, newErrs := registry.LoadDir(*dir)
			if newReg == nil || len(newErrs) > 0 {
				printErrs("reload", newErrs)
				log.Printf("reload: keeping previous registry (%d problem(s))", len(newErrs))
				continue
			}
			srv.SetRegistry(newReg)
			log.Printf("reload: ok (%d queries)", len(newReg.Names()))
		}
	}()
	defer func() {
		signal.Stop(hup)
		close(hup) // releases the reload goroutine
	}()

	httpSrv := &http.Server{Addr: addr, Handler: srv.Handler()}
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	fmt.Printf("plinth serving %d queries on %s\n", len(reg.Names()), addr)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return &DBError{Err: err}
		}
		return nil
	case sig := <-stop:
		log.Printf("shutting down on %s", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			return &DBError{Err: fmt.Errorf("shutdown: %w", err)}
		}
		<-errCh // ListenAndServe has now returned ErrServerClosed
		return nil
	}
}

// runStatus is informational and never fails: query count (with load
// problems as notes) plus the tail of the audit log.
func runStatus(args []string) error {
	_, dir, err := newFlagSet("status", args, nil)
	if err != nil {
		return err
	}
	reg, errs := registry.LoadDir(*dir)
	if reg == nil {
		fmt.Println("queries: 0 (none loaded)")
	} else {
		fmt.Printf("queries: %d (%s)\n", len(reg.Names()), strings.Join(reg.Names(), ", "))
	}
	printErrs("note", errs)
	cfg, err := loadConfig(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "note:", err)
		return nil
	}
	tailAudit(cfg.Audit.Path)
	return nil
}

// tailAudit prints the last ≤5 non-empty audit lines as-is.
func tailAudit(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // no audit file yet — nothing to show
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if ln := sc.Text(); ln != "" {
			lines = append(lines, ln)
		}
	}
	start := max(len(lines)-5, 0)
	for _, ln := range lines[start:] {
		fmt.Println(ln)
	}
}
