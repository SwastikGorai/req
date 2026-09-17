package scripting

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/dop251/goja"

	"req/internal/httpclient"
	"req/internal/variables"
)

// maxSpikeBody bounds what a script-bound request may read in this spike;
// the configurable 10 MiB policy arrives with the production bindings.
const maxSpikeBody = 1 << 20

// errSkipRequest is the sentinel thrown by pm.execution.skipRequest(). A
// script may catch it, but the skip flag the binding set always wins.
var errSkipRequest = errors.New("skipRequest")

// engine runs one goja.Runtime on a single owner goroutine. Every JS
// interaction happens on that goroutine; the only cross-goroutine runtime
// call is Runtime.Interrupt from the deadline watchdog, which Goja documents
// for exactly this use. Host HTTP workers return results through the job
// queue and never touch runtime values directly.
type engine struct {
	rt        *goja.Runtime
	jobs      chan func()
	quit      chan struct{}
	ownerDone chan struct{}
	closeOnce sync.Once

	httpClient *http.Client
	pending    atomic.Int64   // host tasks in flight, counted before launch
	workers    sync.WaitGroup // in-flight host goroutines, for Close

	// Fields below are owned by the owner goroutine only.
	logs          []string
	logBytes      int
	logsTruncated bool            // a console cap was hit; the notice is appended once
	tests         []TestResult    // pm.test outcomes, reset at the start of each run
	runCtx        context.Context // active run's context, captured by host bindings
	running       bool
	runGen        uint64 // increments per Run; identifies late completions
	runErr        error  // first runtime error from a callback, fails the run
	skipRequested bool   // set by pm.execution.skipRequest, survives a caught sentinel

	// The pm surface's phase state. Set*Request methods are called by the
	// execution between runs, when no script can be on the stack.
	scope    *variables.Scope
	srcName  string // current script's Name, set per Run
	phase    string // phasePre, phasePost or empty
	pre      *ExecRequest
	post     *ExecRequest
	resp     *ResponseData
	pmTarget *goja.Object // the unguarded pm object backing the pm proxy
}

// NewEngine starts an engine with the production pm surface and console,
// bound to scope, plus its owner goroutine. A nil scope becomes an empty
// one. Close must be called. VM global state is shared between the runs of
// one engine but never across engines, so one execution gets exactly one
// engine.
func NewEngine(scope *variables.Scope) *engine {
	if scope == nil {
		scope = &variables.Scope{}
	}
	e := &engine{
		rt:         goja.New(),
		jobs:       make(chan func()),
		quit:       make(chan struct{}),
		ownerDone:  make(chan struct{}),
		httpClient: httpclient.DefaultClient(),
		scope:      scope,
	}
	e.installBindings()
	go e.loop()
	return e
}

func (e *engine) loop() {
	defer close(e.ownerDone)
	for {
		select {
		case job := <-e.jobs:
			job()
		case <-e.quit:
			return
		}
	}
}

// enqueue submits a closure for the owner goroutine. It never blocks past
// Close: after quit, late completions are discarded.
func (e *engine) enqueue(job func()) bool {
	select {
	case e.jobs <- job:
		return true
	case <-e.quit:
		return false
	}
}

type runResult struct {
	report Report
	err    error
}

// Run executes src on the owner goroutine and drains its asynchronous work.
// The watchdog interrupts running JS when ctx ends; a latched interrupt from
// a race is cleared at the start of the next run, after the previous
// watchdog has provably exited.
func (e *engine) Run(ctx context.Context, src Source) (Report, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	watchStop := make(chan struct{})
	watchExited := make(chan struct{})
	go func() {
		defer close(watchExited)
		select {
		case <-ctx.Done():
			e.rt.Interrupt(ctx.Err())
		case <-watchStop:
		}
	}()
	defer func() { close(watchStop); <-watchExited }()

	resCh := make(chan runResult, 1)
	if !e.enqueue(func() { resCh <- e.runOnOwner(runCtx, src) }) {
		return Report{}, errors.New("scripting: engine is closed")
	}
	res := <-resCh
	if res.err == nil {
		if err := ctx.Err(); err != nil {
			return Report{}, err // late success is discarded after cancellation
		}
	}
	return res.report, res.err
}

// runOnOwner must run on the owner goroutine.
func (e *engine) runOnOwner(ctx context.Context, src Source) runResult {
	e.rt.ClearInterrupt()
	e.logs, e.logBytes, e.logsTruncated = nil, 0, false
	e.tests = nil
	e.runErr, e.running = nil, true
	e.skipRequested = false
	e.srcName = src.Name
	e.runCtx, e.runGen = ctx, e.runGen+1
	e.pending.Store(0)

	prg, err := goja.Compile(src.Name, src.Code, false)
	if err == nil {
		_, err = e.rt.RunProgram(prg)
	}
	// A skip terminates the script's pending work, so drain is not run.
	// There is no asynchronous production API yet; this is bookkeeping only.
	if err == nil && !e.skipRequested {
		err = e.drain(ctx)
	}
	if err == nil {
		err = e.runErr
	}
	e.running, e.runCtx = false, nil
	if e.skipRequested {
		// The skip flag wins over a later runtime error: a script may have
		// caught the skipRequest sentinel after calling it.
		return runResult{Report{Logs: e.logs, Tests: e.tests, Skipped: true}, nil}
	}
	if err != nil {
		return runResult{Report{}, err}
	}
	return runResult{Report{Logs: e.logs, Tests: e.tests}, nil}
}

// drain settles tracked asynchronous work. Goja runs queued promise reaction
// jobs automatically whenever the JS stack empties (after RunProgram and
// after each owner-thread callback), so pending == 0 means everything has
// settled — including reactions that scheduled more host work, because those
// raised pending again before the check.
func (e *engine) drain(ctx context.Context) error {
	for {
		if err := e.runErr; err != nil {
			return err
		}
		if e.pending.Load() == 0 {
			return ctx.Err()
		}
		select {
		case job := <-e.jobs:
			job()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (e *engine) Close() error {
	e.closeOnce.Do(func() { close(e.quit) })
	e.workers.Wait() // host goroutines finish because run contexts are canceled
	<-e.ownerDone
	return nil
}

type httpResult struct {
	status int
	body   string
	err    error
}

// startHTTP launches one host request whose completion closure runs on the
// owner goroutine. pending is counted before launch and decremented only
// after the owner-thread callback settles.
func (e *engine) startHTTP(url string, settle func(httpResult)) {
	ctx, gen := e.runCtx, e.runGen
	e.pending.Add(1)
	e.workers.Add(1)
	go func() {
		defer e.workers.Done()
		res := e.fetch(ctx, url)
		e.enqueue(func() {
			if !e.running || gen != e.runGen {
				return // late completion after its run ended: discard
			}
			defer e.pending.Add(-1)
			settle(res)
		})
	}()
}

func (e *engine) fetch(ctx context.Context, url string) httpResult {
	resp, err := httpclient.Send(ctx, e.httpClient, http.MethodGet, url, nil, nil)
	if err != nil {
		return httpResult{err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSpikeBody))
	if err != nil {
		return httpResult{status: resp.StatusCode, err: fmt.Errorf("reading body: %w", err)}
	}
	return httpResult{status: resp.StatusCode, body: string(body)}
}

func (e *engine) setRunErr(err error) {
	if e.runErr == nil {
		e.runErr = err
	}
}

func (r httpResult) errValue(rt *goja.Runtime) goja.Value {
	if r.err == nil {
		return goja.Null()
	}
	return rt.ToValue(r.err.Error())
}

func (r httpResult) value(rt *goja.Runtime) goja.Value {
	obj := rt.NewObject()
	_ = obj.Set("status", r.status)
	_ = obj.Set("body", r.body)
	return obj
}
