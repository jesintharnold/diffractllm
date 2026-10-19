package settings

import (
	"diffractllm/internal/observability/logging"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// Field is one editable setting of a settings struct S.
type Field[S any] interface {
	Key() string
	problem(s *S) string
	same(a, b *S) bool
	value(s *S) any
	set(dst *S, raw json.RawMessage) error
	assign(dst, src *S)
	hook() func(S) error
}

type field[S any, V int64 | bool | string] struct {
	key   string
	ptr   func(*S) *V
	check func(V) string
}

func (f field[S, V]) Key() string         { return f.key }
func (f field[S, V]) problem(s *S) string { return f.check(*f.ptr(s)) }
func (f field[S, V]) same(a, b *S) bool   { return *f.ptr(a) == *f.ptr(b) }
func (f field[S, V]) value(s *S) any      { return *f.ptr(s) }
func (f field[S, V]) assign(dst, src *S)  { *f.ptr(dst) = *f.ptr(src) }
func (f field[S, V]) hook() func(S) error { return nil }
func (f field[S, V]) set(dst *S, raw json.RawMessage) error {
	var v V
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*f.ptr(dst) = v
	return nil
}

// Int is a whole number that must stay within [min, max].
func Int[S any](key string, ptr func(*S) *int64, min, max int64) Field[S] {
	return field[S, int64]{key, ptr, func(v int64) string {
		if v < min || v > max {
			return fmt.Sprintf("must be between %d and %d", min, max)
		}
		return ""
	}}
}

func Bool[S any](key string, ptr func(*S) *bool) Field[S] {
	return field[S, bool]{key, ptr, func(bool) string { return "" }}
}

// Choice is one of a fixed set of words.
func Choice[S any](key string, ptr func(*S) *string, options ...string) Field[S] {
	return field[S, string]{key, ptr, func(v string) string {
		if slices.Contains(options, v) {
			return ""
		}
		return "must be one of " + strings.Join(options, ", ")
	}}
}

type live[S any] struct {
	Field[S]
	apply func(S) error
}

func (l live[S]) hook() func(S) error { return l.apply }

// Live marks a field as applied to the running gateway when it is saved: apply gets the new
// settings. A field without it applies at the next start, and the console says so.
func Live[S any](f Field[S], apply func(S) error) Field[S] { return live[S]{f, apply} }

// Spec lists a page's fields. Cross reports rules that span several fields, by key; it may be nil
// and must cope with values that fail their own field check, since it runs alongside them.
type Spec[S any] struct {
	Fields []Field[S]
	Cross  func(S) map[string]string
}

// Error names what is wrong per field, so the console can show each under its input.
type Error struct{ Fields map[string]string }

func (e *Error) Error() string {
	keys := slices.Sorted(maps.Keys(e.Fields))
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + ": " + e.Fields[k]
	}
	return "invalid settings: " + strings.Join(parts, "; ")
}

var (
	ErrUnknownScope = errors.New("unknown settings page")
	ErrBadForm      = errors.New("the form is not valid JSON")
	// ErrNotApplied means the value is saved but a live field could not be applied to the running
	// gateway; it takes effect at the next start.
	ErrNotApplied = errors.New("saved, but not applied to the running gateway")
)

func (sp Spec[S]) Validate(s S) error {
	problems := map[string]string{}
	for _, f := range sp.Fields {
		if msg := f.problem(&s); msg != "" {
			problems[f.Key()] = msg
		}
	}
	if sp.Cross != nil {
		for key, msg := range sp.Cross(s) {
			if _, already := problems[key]; !already {
				problems[key] = msg
			}
		}
	}
	if len(problems) > 0 {
		return &Error{Fields: problems}
	}
	return nil
}

// Diff is what differs from the defaults: the only thing that is saved.
func (sp Spec[S]) Diff(defaults, s S) map[string]any {
	out := map[string]any{}
	for _, f := range sp.Fields {
		if !f.same(&defaults, &s) {
			out[f.Key()] = f.value(&s)
		}
	}
	return out
}

func (sp Spec[S]) Resolve(defaults S, saved []byte, log *zap.Logger) S {
	if len(saved) == 0 {
		return defaults
	}
	var overrides map[string]json.RawMessage
	if err := json.Unmarshal(saved, &overrides); err != nil {
		log.Warn("saved settings are unreadable, using server.yaml", zap.Error(err))
		return defaults
	}
	type applied struct {
		key      string
		yaml, to any
	}
	var took []applied
	out := defaults
	known := map[string]bool{}
	for _, f := range sp.Fields {
		known[f.Key()] = true
		raw, ok := overrides[f.Key()]
		if !ok {
			continue
		}
		next := out
		if err := f.set(&next, raw); err != nil {
			log.Warn("saved setting is not readable, using server.yaml", zap.String("setting", f.Key()), zap.Error(err))
			continue
		}
		if msg := f.problem(&next); msg != "" {
			log.Warn("saved setting is not valid, using server.yaml", zap.String("setting", f.Key()), zap.String("problem", msg))
			continue
		}
		took = append(took, applied{f.Key(), f.value(&defaults), f.value(&next)})
		out = next
	}
	for key := range overrides {
		if !known[key] {
			log.Warn("saved setting is not a known setting, ignored", zap.String("setting", key))
		}
	}
	if sp.Cross != nil {
		if bad := sp.Cross(out); len(bad) > 0 {
			log.Warn("saved settings contradict each other, using server.yaml", zap.Any("problems", bad))
			return defaults
		}
	}
	for _, a := range took {
		log.Info("setting overrides server.yaml", zap.String("setting", a.key), zap.Any("yaml", a.yaml), zap.Any("override", a.to))
	}
	return out
}

type Store interface {
	GetSettingOverrides(scope string) ([]byte, error)
	SaveSettingOverrides(scope string, data []byte) error
	DeleteSettingOverrides(scope string) error
}

type View[S any] struct {
	Settings        S        `json:"settings"`
	Defaults        S        `json:"defaults"`
	RestartRequired []string `json:"restart_required"`
	Applied         []string `json:"applied,omitempty"`
}

type Registry struct {
	store  Store
	log    *zap.Logger
	mu     sync.RWMutex
	scopes map[string]scope
}

type scope interface {
	view() (any, error)
	save(raw []byte) (any, error)
	reset() (any, error)
}

func NewRegistry(store Store) *Registry {
	return &Registry{store: store, log: logging.For("settings"), scopes: map[string]scope{}}
}

func (r *Registry) get(name string) (scope, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.scopes[name]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownScope, name)
	}
	return s, nil
}

func (r *Registry) View(name string) (any, error) {
	s, err := r.get(name)
	if err != nil {
		return nil, err
	}
	return s.view()
}

// Save takes a whole form as JSON and stores what differs from yaml.
func (r *Registry) Save(name string, form []byte) (any, error) {
	s, err := r.get(name)
	if err != nil {
		return nil, err
	}
	return s.save(form)
}

// Reset drops every override of a page.
func (r *Registry) Reset(name string) (any, error) {
	s, err := r.get(name)
	if err != nil {
		return nil, err
	}
	return s.reset()
}

// Handle is a registered page, typed. Its owner reads Running at startup to build itself.
type Handle[S any] struct {
	name     string
	spec     Spec[S]
	defaults S
	reg      *Registry
	mu       sync.Mutex
	running  S
}

func Register[S any](r *Registry, name string, spec Spec[S], defaults S) (*Handle[S], error) {
	if err := spec.Validate(defaults); err != nil {
		return nil, err
	}
	raw, err := r.store.GetSettingOverrides(name)
	if err != nil {
		return nil, err
	}
	h := &Handle[S]{name: name, spec: spec, defaults: defaults, reg: r, running: spec.Resolve(defaults, raw, r.log)}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, taken := r.scopes[name]; taken {
		return nil, fmt.Errorf("settings page %q is registered twice", name)
	}
	r.scopes[name] = h
	return h, nil
}

func (h *Handle[S]) Running() S {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running
}

func (h *Handle[S]) Defaults() S { return h.defaults }

// build reports saved against running. The caller holds h.mu.
func (h *Handle[S]) build(saved S, applied []string) View[S] {
	restart := []string{}
	for _, f := range h.spec.Fields {
		if !f.same(&h.running, &saved) {
			restart = append(restart, f.Key())
		}
	}
	return View[S]{Settings: saved, Defaults: h.defaults, RestartRequired: restart, Applied: applied}
}

func (h *Handle[S]) Saved() (View[S], error) {
	raw, err := h.reg.store.GetSettingOverrides(h.name)
	if err != nil {
		return View[S]{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.build(h.spec.Resolve(h.defaults, raw, zap.NewNop()), nil), nil
}

func (h *Handle[S]) Save(next S) (View[S], error) {
	if err := h.spec.Validate(next); err != nil {
		return View[S]{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	changed := h.spec.Diff(h.defaults, next)
	if len(changed) == 0 {
		return h.resetLocked()
	}
	raw, err := json.Marshal(changed)
	if err != nil {
		return View[S]{}, err
	}
	if err := h.reg.store.SaveSettingOverrides(h.name, raw); err != nil {
		return View[S]{}, err
	}
	return h.applyLocked(h.spec.Resolve(h.defaults, raw, zap.NewNop()))
}

// Reset drops every override, so yaml applies again: now for live fields, at the next start for the rest.
func (h *Handle[S]) Reset() (View[S], error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.resetLocked()
}

func (h *Handle[S]) resetLocked() (View[S], error) {
	if err := h.reg.store.DeleteSettingOverrides(h.name); err != nil {
		return View[S]{}, err
	}
	return h.applyLocked(h.defaults)
}

// applyLocked runs the live hooks of the fields that changed. A hook that fails leaves that field
// pending a restart and is reported; the value is saved either way.
func (h *Handle[S]) applyLocked(saved S) (View[S], error) {
	var applied, failed []string
	for _, f := range h.spec.Fields {
		apply := f.hook()
		if apply == nil || f.same(&h.running, &saved) {
			continue
		}
		if err := apply(saved); err != nil {
			failed = append(failed, f.Key()+": "+err.Error())
			continue
		}
		f.assign(&h.running, &saved)
		applied = append(applied, f.Key())
	}
	view := h.build(saved, applied)
	if len(failed) > 0 {
		return view, fmt.Errorf("%w: %s", ErrNotApplied, strings.Join(failed, "; "))
	}
	return view, nil
}

func (h *Handle[S]) view() (any, error) {
	v, err := h.Saved()
	return v, err
}

func (h *Handle[S]) reset() (any, error) {
	v, err := h.Reset()
	return v, err
}

// save reads a whole form. Every field must be present, so a partial form cannot silently turn
// something off by leaving it out.
func (h *Handle[S]) save(raw []byte) (any, error) {
	var present map[string]json.RawMessage
	if err := json.Unmarshal(raw, &present); err != nil {
		return nil, errors.Join(ErrBadForm, err)
	}
	missing := map[string]string{}
	for _, f := range h.spec.Fields {
		if _, ok := present[f.Key()]; !ok {
			missing[f.Key()] = "is required"
		}
	}
	if len(missing) > 0 {
		return nil, &Error{Fields: missing}
	}
	var next S
	if err := json.Unmarshal(raw, &next); err != nil {
		return nil, errors.Join(ErrBadForm, err)
	}
	v, err := h.Save(next)
	return v, err
}
