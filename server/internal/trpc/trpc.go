// Package trpc serves the tRPC v11 HTTP wire protocol (httpBatchLink +
// superjson envelope) so the existing React client keeps working unchanged.
//
//	query:    GET  /api/trpc/a.b,c.d?batch=1&input={"0":{"json":...},"1":...}
//	mutation: POST /api/trpc/a.b?batch=1   body {"0":{"json":...}}
//	response: [{"result":{"data":{"json":...}}}, {"error":{"json":{...}}}]
//
// Outputs are emitted without superjson meta: time.Time becomes an ISO string,
// which the client already wraps in new Date(...).
package trpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"lingxi/server/internal/apperr"
)

type Kind int

const (
	KindQuery Kind = iota
	KindMutation
)

type Handler func(ctx context.Context, input json.RawMessage) (any, error)

type proc struct {
	kind   Kind
	public bool
	fn     Handler
}

// Hooks lets the HTTP layer plug in auth, rate limiting and auditing without
// this package knowing about sessions.
type Hooks struct {
	// Authenticated reports whether ctx carries a logged-in user.
	Authenticated func(ctx context.Context) bool
	// BeforeMutation may reject (e.g. rate limit) before the handler runs.
	BeforeMutation func(ctx context.Context, path string) error
	// AfterMutation records the outcome (audit log). input is the raw JSON.
	AfterMutation func(ctx context.Context, path string, input json.RawMessage, err error)
}

type Router struct {
	procs map[string]proc
	hooks Hooks
}

const (
	maxBatch     = 20
	maxBodyBytes = 1 << 20
)

func NewRouter(h Hooks) *Router { return &Router{procs: map[string]proc{}, hooks: h} }

func (r *Router) add(path string, p proc) {
	if _, dup := r.procs[path]; dup {
		panic("trpc: duplicate procedure " + path)
	}
	r.procs[path] = p
}

func (r *Router) Query(path string, fn Handler)    { r.add(path, proc{kind: KindQuery, fn: fn}) }
func (r *Router) Mutation(path string, fn Handler) { r.add(path, proc{kind: KindMutation, fn: fn}) }
func (r *Router) PublicQuery(path string, fn Handler) {
	r.add(path, proc{kind: KindQuery, public: true, fn: fn})
}

// Validator is implemented by input structs that check their own invariants.
type Validator interface{ Validate() error }

// Typed adapts a strongly typed function. Input decoding is strict: unknown
// fields and wrong types are rejected rather than silently zeroed.
func Typed[In any, Out any](fn func(ctx context.Context, in In) (Out, error)) Handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var in In
		if len(raw) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&in); err != nil {
				return nil, apperr.Bad("参数格式错误：%s", sanitizeDecodeErr(err))
			}
			if dec.More() {
				return nil, apperr.Bad("参数格式错误")
			}
		}
		if v, ok := any(&in).(Validator); ok {
			if err := v.Validate(); err != nil {
				return nil, err
			}
		}
		return fn(ctx, in)
	}
}

// NoInput adapts a function without parameters.
func NoInput[Out any](fn func(ctx context.Context) (Out, error)) Handler {
	return func(ctx context.Context, _ json.RawMessage) (any, error) { return fn(ctx) }
}

func sanitizeDecodeErr(err error) string {
	var ute *json.UnmarshalTypeError
	if errors.As(err, &ute) {
		return "字段 " + ute.Field + " 类型不正确"
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "json: unknown field ") {
		return "不支持的字段 " + strings.TrimPrefix(msg, "json: unknown field ")
	}
	return "JSON 无法解析"
}

type envelope struct {
	JSON json.RawMessage `json:"json"`
	Meta json.RawMessage `json:"meta,omitempty"`
}

type errorShape struct {
	Message string    `json:"message"`
	Code    int       `json:"code"`
	Data    errorData `json:"data"`
}

type errorData struct {
	Code       apperr.Code `json:"code"`
	HTTPStatus int         `json:"httpStatus"`
	Path       string      `json:"path,omitempty"`
}

type item struct {
	Result *struct {
		Data envelope `json:"data"`
	} `json:"result,omitempty"`
	Error *struct {
		JSON errorShape `json:"json"`
	} `json:"error,omitempty"`
	status int
}

// ServeHTTP handles everything under the mount prefix; the procedure list is
// the last path segment.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	paths := strings.Split(lastSegment(req.URL.Path), ",")
	batch := req.URL.Query().Get("batch") == "1"
	if len(paths) == 0 || paths[0] == "" {
		writeItems(w, []item{errItem(apperr.NotFoundf("未知接口"), "")}, batch)
		return
	}
	if len(paths) > maxBatch || (!batch && len(paths) > 1) {
		writeItems(w, []item{errItem(apperr.Bad("批量请求过多"), "")}, batch)
		return
	}

	var kind Kind
	switch req.Method {
	case http.MethodGet:
		kind = KindQuery
	case http.MethodPost:
		kind = KindMutation
	default:
		writeItems(w, []item{errItem(apperr.New(apperr.MethodNotAllow, "不支持的请求方法"), "")}, batch)
		return
	}

	inputs, err := readInputs(req, batch, len(paths))
	if err != nil {
		writeItems(w, []item{errItem(err, "")}, batch)
		return
	}

	items := make([]item, len(paths))
	for i, path := range paths {
		items[i] = r.call(req.Context(), kind, path, inputs[i])
	}
	writeItems(w, items, batch)
}

func (r *Router) call(ctx context.Context, kind Kind, path string, in json.RawMessage) (it item) {
	p, ok := r.procs[path]
	if !ok {
		return errItem(apperr.NotFoundf("未知接口 %s", path), path)
	}
	if p.kind != kind {
		return errItem(apperr.New(apperr.MethodNotAllow, "请求方法与接口类型不符"), path)
	}
	if !p.public && (r.hooks.Authenticated == nil || !r.hooks.Authenticated(ctx)) {
		return errItem(apperr.ErrUnauthorized, path)
	}
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "trpc procedure panic", "path", path, "panic", rec)
			it = errItem(apperr.ErrInternal, path)
		}
	}()

	if kind == KindMutation && r.hooks.BeforeMutation != nil {
		if err := r.hooks.BeforeMutation(ctx, path); err != nil {
			return errItem(err, path)
		}
	}
	out, err := p.fn(ctx, in)
	if kind == KindMutation && r.hooks.AfterMutation != nil {
		r.hooks.AfterMutation(ctx, path, in, err)
	}
	if err != nil {
		if _, known := apperr.As(err); !known {
			slog.ErrorContext(ctx, "trpc procedure failed", "path", path, "error", err)
		}
		return errItem(err, path)
	}
	data, err := json.Marshal(out)
	if err != nil {
		slog.ErrorContext(ctx, "trpc marshal failed", "path", path, "error", err)
		return errItem(apperr.ErrInternal, path)
	}
	it.Result = &struct {
		Data envelope `json:"data"`
	}{Data: envelope{JSON: data}}
	it.status = http.StatusOK
	return it
}

func readInputs(req *http.Request, batch bool, n int) ([]json.RawMessage, error) {
	var raw []byte
	if req.Method == http.MethodGet {
		raw = []byte(req.URL.Query().Get("input"))
	} else {
		body, err := io.ReadAll(io.LimitReader(req.Body, maxBodyBytes+1))
		if err != nil {
			return nil, apperr.Bad("请求体读取失败")
		}
		if len(body) > maxBodyBytes {
			return nil, apperr.New(apperr.PayloadTooLarge, "请求体过大")
		}
		raw = body
	}
	if !utf8.Valid(raw) {
		return nil, apperr.New(apperr.ParseError, "请求包含非法字符编码，请使用 UTF-8")
	}
	out := make([]json.RawMessage, n)
	if len(bytes.TrimSpace(raw)) == 0 {
		return out, nil
	}
	if !batch {
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, apperr.New(apperr.ParseError, "请求无法解析")
		}
		out[0] = env.JSON
		return out, nil
	}
	var m map[string]envelope
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, apperr.New(apperr.ParseError, "请求无法解析")
	}
	for k, env := range m {
		i, err := strconv.Atoi(k)
		if err != nil || i < 0 || i >= n {
			return nil, apperr.Bad("批量请求下标非法")
		}
		out[i] = env.JSON
	}
	return out, nil
}

func errItem(err error, path string) item {
	e, ok := apperr.As(err)
	if !ok {
		e = apperr.ErrInternal
	}
	st := e.Code.HTTPStatus()
	return item{
		Error: &struct {
			JSON errorShape `json:"json"`
		}{JSON: errorShape{
			Message: e.Message,
			Code:    e.Code.RPCCode(),
			Data:    errorData{Code: e.Code, HTTPStatus: st, Path: path},
		}},
		status: st,
	}
}

func writeItems(w http.ResponseWriter, items []item, batch bool) {
	status := items[0].status
	for _, it := range items[1:] {
		if it.status != status {
			status = http.StatusMultiStatus
			break
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if batch {
		_ = json.NewEncoder(w).Encode(items)
		return
	}
	_ = json.NewEncoder(w).Encode(items[0])
}

func lastSegment(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
