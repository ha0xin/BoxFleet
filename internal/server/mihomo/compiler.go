// Package mihomo compiles BoxFleet-generated proxies and ordered administrator
// rewrites into a complete Mihomo profile.
package mihomo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dop251/goja"
	"go.yaml.in/yaml/v3"
)

type RewriteKind string

const (
	RewriteYAML       RewriteKind = "yaml"
	RewriteJavaScript RewriteKind = "javascript"
)

type Rewrite struct {
	Name    string      `json:"name"`
	Kind    RewriteKind `json:"kind"`
	Content string      `json:"content"`
}

type Limits struct {
	MaxSourceBytes int
	MaxInputBytes  int
	MaxOutputBytes int
	MaxLogBytes    int
	ScriptTimeout  time.Duration
}

func DefaultLimits() Limits {
	return Limits{
		MaxSourceBytes: 256 << 10,
		MaxInputBytes:  4 << 20,
		MaxOutputBytes: 4 << 20,
		MaxLogBytes:    64 << 10,
		ScriptTimeout:  500 * time.Millisecond,
	}
}

type ErrorKind string

const (
	ErrorInvalidProfile   ErrorKind = "invalid_profile"
	ErrorInvalidRewrite   ErrorKind = "invalid_rewrite"
	ErrorInvalidScript    ErrorKind = "invalid_script"
	ErrorInvalidResult    ErrorKind = "invalid_result"
	ErrorAsyncUnsupported ErrorKind = "async_unsupported"
	ErrorTimeout          ErrorKind = "timeout"
	ErrorCanceled         ErrorKind = "canceled"
	ErrorLimitExceeded    ErrorKind = "limit_exceeded"
)

type CompileError struct {
	Kind    ErrorKind
	Rewrite string
	Err     error
}

func (e *CompileError) Error() string {
	location := "profile"
	if e.Rewrite != "" {
		location = fmt.Sprintf("rewrite %q", e.Rewrite)
	}
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", location, e.Kind)
	}
	return fmt.Sprintf("%s: %s: %v", location, e.Kind, e.Err)
}

func (e *CompileError) Unwrap() error { return e.Err }

type LogEntry struct {
	Rewrite string `json:"rewrite"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type CompileResult struct {
	YAML        []byte       `json:"-"`
	Logs        []LogEntry   `json:"logs"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type Compiler struct {
	limits Limits
}

func NewCompiler(limits Limits) *Compiler {
	defaults := DefaultLimits()
	if limits.MaxSourceBytes <= 0 {
		limits.MaxSourceBytes = defaults.MaxSourceBytes
	}
	if limits.MaxInputBytes <= 0 {
		limits.MaxInputBytes = defaults.MaxInputBytes
	}
	if limits.MaxOutputBytes <= 0 {
		limits.MaxOutputBytes = defaults.MaxOutputBytes
	}
	if limits.MaxLogBytes <= 0 {
		limits.MaxLogBytes = defaults.MaxLogBytes
	}
	if limits.ScriptTimeout <= 0 {
		limits.ScriptTimeout = defaults.ScriptTimeout
	}
	return &Compiler{limits: limits}
}

func (c *Compiler) Compile(ctx context.Context, baseYAML []byte, rewrites []Rewrite) (CompileResult, error) {
	if len(baseYAML) > c.limits.MaxInputBytes {
		return CompileResult{}, &CompileError{Kind: ErrorLimitExceeded, Err: errors.New("base profile is too large")}
	}
	profile, err := decodeProfile(baseYAML)
	if err != nil {
		return CompileResult{}, &CompileError{Kind: ErrorInvalidProfile, Err: err}
	}

	result := CompileResult{}
	logBytes := 0
	for _, rewrite := range rewrites {
		if err := ctx.Err(); err != nil {
			return CompileResult{}, &CompileError{Kind: ErrorCanceled, Rewrite: rewrite.Name, Err: err}
		}
		if len(rewrite.Content) > c.limits.MaxSourceBytes {
			return CompileResult{}, &CompileError{Kind: ErrorLimitExceeded, Rewrite: rewrite.Name, Err: errors.New("source is too large")}
		}

		switch rewrite.Kind {
		case RewriteYAML:
			patch, err := decodeProfile([]byte(rewrite.Content))
			if err != nil {
				return CompileResult{}, &CompileError{Kind: ErrorInvalidRewrite, Rewrite: rewrite.Name, Err: err}
			}
			mergeMap(profile, patch)
		case RewriteJavaScript:
			profile, err = c.runJavaScript(ctx, profile, rewrite, &result.Logs, &logBytes)
			if err != nil {
				return CompileResult{}, err
			}
		default:
			return CompileResult{}, &CompileError{
				Kind: ErrorInvalidRewrite, Rewrite: rewrite.Name,
				Err: fmt.Errorf("unsupported kind %q", rewrite.Kind),
			}
		}

		encoded, err := yaml.Marshal(profile)
		if err != nil {
			return CompileResult{}, &CompileError{Kind: ErrorInvalidResult, Rewrite: rewrite.Name, Err: err}
		}
		if len(encoded) > c.limits.MaxOutputBytes {
			return CompileResult{}, &CompileError{Kind: ErrorLimitExceeded, Rewrite: rewrite.Name, Err: errors.New("output is too large")}
		}
	}

	result.YAML, err = yaml.Marshal(profile)
	if err != nil {
		return CompileResult{}, &CompileError{Kind: ErrorInvalidResult, Err: err}
	}
	if len(result.YAML) > c.limits.MaxOutputBytes {
		return CompileResult{}, &CompileError{Kind: ErrorLimitExceeded, Err: errors.New("output is too large")}
	}
	return result, nil
}

func MergeYAML(baseYAML, patchYAML []byte) ([]byte, error) {
	base, err := decodeProfile(baseYAML)
	if err != nil {
		return nil, fmt.Errorf("decode base profile: %w", err)
	}
	patch, err := decodeProfile(patchYAML)
	if err != nil {
		return nil, fmt.Errorf("decode patch: %w", err)
	}
	mergeMap(base, patch)
	return yaml.Marshal(base)
}

func decodeProfile(raw []byte) (map[string]any, error) {
	var value any
	if err := yaml.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	profile, ok := value.(map[string]any)
	if !ok || profile == nil {
		return nil, errors.New("YAML root must be an object")
	}
	return profile, nil
}

func mergeMap(target, patch map[string]any) {
	for key, value := range patch {
		switch typed := value.(type) {
		case map[string]any:
			if strings.HasSuffix(key, "!") {
				target[unwrapKey(strings.TrimSuffix(key, "!"))] = typed
				continue
			}
			actual := unwrapKey(key)
			nested, ok := target[actual].(map[string]any)
			if !ok {
				nested = make(map[string]any)
				target[actual] = nested
			}
			mergeMap(nested, typed)
		case []any:
			switch {
			case strings.HasPrefix(key, "+"):
				actual := unwrapKey(strings.TrimPrefix(key, "+"))
				current, _ := target[actual].([]any)
				target[actual] = append(append(make([]any, 0, len(typed)+len(current)), typed...), current...)
			case strings.HasSuffix(key, "+"):
				actual := unwrapKey(strings.TrimSuffix(key, "+"))
				current, _ := target[actual].([]any)
				target[actual] = append(append(make([]any, 0, len(current)+len(typed)), current...), typed...)
			default:
				target[unwrapKey(key)] = typed
			}
		default:
			target[key] = value
		}
	}
}

func unwrapKey(key string) string {
	if strings.HasPrefix(key, "<") && strings.HasSuffix(key, ">") {
		return strings.TrimSuffix(strings.TrimPrefix(key, "<"), ">")
	}
	return key
}

func (c *Compiler) runJavaScript(
	ctx context.Context,
	profile map[string]any,
	rewrite Rewrite,
	logs *[]LogEntry,
	logBytes *int,
) (map[string]any, error) {
	vm := goja.New()
	console := vm.NewObject()
	for _, level := range []string{"log", "info", "warn", "error", "debug"} {
		level := level
		if err := console.Set(level, func(call goja.FunctionCall) goja.Value {
			parts := make([]string, 0, len(call.Arguments))
			for _, argument := range call.Arguments {
				parts = append(parts, argument.String())
			}
			c.appendLog(logs, logBytes, LogEntry{
				Rewrite: rewrite.Name,
				Level:   level,
				Message: strings.Join(parts, " "),
			})
			return goja.Undefined()
		}); err != nil {
			return nil, &CompileError{Kind: ErrorInvalidScript, Rewrite: rewrite.Name, Err: err}
		}
	}
	if err := vm.Set("console", console); err != nil {
		return nil, &CompileError{Kind: ErrorInvalidScript, Rewrite: rewrite.Name, Err: err}
	}

	runCtx, cancel := context.WithTimeout(ctx, c.limits.ScriptTimeout)
	defer cancel()
	done := make(chan struct{})
	go func() {
		select {
		case <-runCtx.Done():
			vm.Interrupt(runCtx.Err())
		case <-done:
		}
	}()
	defer close(done)

	if _, err := vm.RunString(rewrite.Content); err != nil {
		return nil, scriptExecutionError(runCtx, rewrite.Name, err)
	}
	main, ok := goja.AssertFunction(vm.Get("main"))
	if !ok {
		return nil, &CompileError{Kind: ErrorInvalidScript, Rewrite: rewrite.Name, Err: errors.New("main(config) is required")}
	}

	cloned, err := cloneProfile(profile)
	if err != nil {
		return nil, &CompileError{Kind: ErrorInvalidProfile, Rewrite: rewrite.Name, Err: err}
	}
	value, err := main(goja.Undefined(), vm.ToValue(cloned))
	if err != nil {
		return nil, scriptExecutionError(runCtx, rewrite.Name, err)
	}
	if _, ok := value.Export().(*goja.Promise); ok {
		return nil, &CompileError{Kind: ErrorAsyncUnsupported, Rewrite: rewrite.Name, Err: errors.New("main(config) must return synchronously")}
	}

	raw, err := json.Marshal(value.Export())
	if err != nil {
		return nil, &CompileError{Kind: ErrorInvalidResult, Rewrite: rewrite.Name, Err: err}
	}
	if len(raw) > c.limits.MaxOutputBytes {
		return nil, &CompileError{Kind: ErrorLimitExceeded, Rewrite: rewrite.Name, Err: errors.New("JavaScript result is too large")}
	}
	var output map[string]any
	if err := json.Unmarshal(raw, &output); err != nil || output == nil {
		if err == nil {
			err = errors.New("main(config) must return an object")
		}
		return nil, &CompileError{Kind: ErrorInvalidResult, Rewrite: rewrite.Name, Err: err}
	}
	return output, nil
}

func scriptExecutionError(ctx context.Context, rewrite string, err error) error {
	if ctx.Err() != nil {
		kind := ErrorTimeout
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			kind = ErrorCanceled
		}
		return &CompileError{Kind: kind, Rewrite: rewrite, Err: ctx.Err()}
	}
	return &CompileError{Kind: ErrorInvalidScript, Rewrite: rewrite, Err: err}
}

func cloneProfile(profile map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(profile)
	if err != nil {
		return nil, err
	}
	var cloned map[string]any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

func (c *Compiler) appendLog(logs *[]LogEntry, used *int, entry LogEntry) {
	if *used >= c.limits.MaxLogBytes {
		return
	}
	remaining := c.limits.MaxLogBytes - *used
	if len(entry.Message) > remaining {
		entry.Message = truncateUTF8(entry.Message, remaining) + " [truncated]"
		*used = c.limits.MaxLogBytes
	} else {
		*used += len(entry.Message)
	}
	*logs = append(*logs, entry)
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) && len(value) > 0 {
		value = value[:len(value)-1]
	}
	return value
}
