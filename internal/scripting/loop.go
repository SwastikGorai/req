package scripting

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/dop251/goja"

	"req/internal/httpclient"
)

// maxSpikeBody bounds what a script-bound request may read in this spike;
// the configurable 10 MiB policy arrives with the production bindings.
const maxSpikeBody = 1 << 20

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
	logs    []string
	runCtx  context.Context // active run's context, captured by host bindings
	running bool
	runGen  uint64 // increments per Run; identifies late completions
	runErr  error  // first runtime error from a callback, fails the run

	vars *varStore
}

// NewEngine starts an engine and its owner goroutine. Close must be called.
func NewEngine() *engine {
	e := &engine{
		rt:         goja.New(),
		jobs:       make(chan func()),
		quit:       make(chan struct{}),
		ownerDone:  make(chan struct{}),
		httpClient: httpclient.DefaultClient(),
		vars:       newVarStore(),
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
	e.logs, e.runErr, e.running = nil, nil, true
	e.runCtx, e.runGen = ctx, e.runGen+1
	e.pending.Store(0)

	prg, err := goja.Compile(src.Name, src.Code, false)
	if err == nil {
		_, err = e.rt.RunProgram(prg)
	}
	if err == nil {
		err = e.drain(ctx)
	}
	if err == nil {
		err = e.runErr
	}
	e.running, e.runCtx = false, nil
	if err != nil {
		return runResult{Report{}, err}
	}
	return runResult{Report{Logs: e.logs}, nil}
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
			defer e.pending.Add(-1)
			if !e.running || gen != e.runGen {
				return // late completion after its run ended: discard
			}
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

// installBindings registers the spike surface: vars (Go-backed store), log,
// httpGet (callback) and httpGetAsync (Promise). None of this is the
// production pm API.
func (e *engine) installBindings() {
	rt := e.rt

	varsObj := rt.NewObject()
	mustSet := func(name string, value interface{}) {
		if err := varsObj.Set(name, value); err != nil {
			panic(err)
		}
	}
	mustSet("get", func(key string) interface{} { return e.vars.get(key) })
	mustSet("set", func(key string, value interface{}) { e.vars.set(key, value) })
	if err := rt.Set("vars", varsObj); err != nil {
		panic(err)
	}

	if err := rt.Set("log", func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for i, arg := range call.Arguments {
			parts[i] = arg.String()
		}
		e.logs = append(e.logs, strings.Join(parts, " "))
		return goja.Undefined()
	}); err != nil {
		panic(err)
	}

	if err := rt.Set("httpGet", func(call goja.FunctionCall) goja.Value {
		url := call.Argument(0).String()
		cb, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(rt.NewTypeError("httpGet(url, callback) requires a string URL and a callback function"))
		}
		e.startHTTP(url, func(res httpResult) {
			_, err := cb(goja.Undefined(), res.errValue(rt), res.value(rt))
			if err != nil && e.runErr == nil {
				e.runErr = fmt.Errorf("httpGet callback: %w", err)
			}
		})
		return goja.Undefined()
	}); err != nil {
		panic(err)
	}

	if err := rt.Set("httpGetAsync", func(call goja.FunctionCall) goja.Value {
		url := call.Argument(0).String()
		promise, resolve, reject := rt.NewPromise()
		e.startHTTP(url, func(res httpResult) {
			if res.err != nil {
				if err := reject(res.err.Error()); err != nil {
					e.setRunErr(err)
				}
				return
			}
			if err := resolve(res.value(rt)); err != nil {
				e.setRunErr(err)
			}
		})
		return rt.ToValue(promise)
	}); err != nil {
		panic(err)
	}
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

// varStore is the spike's JS-to-Go variable adapter.
type varStore struct {
	mu sync.Mutex
	m  map[string]interface{}
}

func newVarStore() *varStore {
	return &varStore{m: make(map[string]interface{})}
}

func (s *varStore) get(key string) interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[key]
}

func (s *varStore) set(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
}
