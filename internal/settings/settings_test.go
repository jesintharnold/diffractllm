package settings

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type page struct {
	Days  int64  `json:"days"`
	Keep  int64  `json:"keep_days"`
	On    bool   `json:"on"`
	Level string `json:"level"`
}

// level is the one live field: saving it applies it at once through liveHook.
var (
	liveSeen []string
	liveErr  error
)

func liveHook(p page) error {
	liveSeen = append(liveSeen, p.Level)
	return liveErr
}

var pageSpec = Spec[page]{
	Fields: []Field[page]{
		Int("days", func(p *page) *int64 { return &p.Days }, 1, 90),
		Int("keep_days", func(p *page) *int64 { return &p.Keep }, 1, 90),
		Bool("on", func(p *page) *bool { return &p.On }),
		Live(Choice("level", func(p *page) *string { return &p.Level }, "debug", "info"), liveHook),
	},
	Cross: func(p page) map[string]string {
		if p.Keep > p.Days {
			return map[string]string{"keep_days": "cannot exceed days"}
		}
		return nil
	},
}

var yaml = page{Days: 30, Keep: 7, On: true, Level: "info"}

func TestValidateNamesEveryBadField(t *testing.T) {
	var bad *Error
	require.ErrorAs(t, pageSpec.Validate(page{Days: 0, Keep: 7, On: true, Level: "loud"}), &bad)

	assert.Equal(t, "must be between 1 and 90", bad.Fields["days"])
	assert.Equal(t, "must be one of debug, info", bad.Fields["level"])
	assert.Equal(t, "cannot exceed days", bad.Fields["keep_days"], "the cross rule is reported with the field problems, not after them")
	assert.Len(t, bad.Fields, 3)
	assert.NoError(t, pageSpec.Validate(yaml))
}

func TestValidateChecksRulesThatSpanFields(t *testing.T) {
	var bad *Error
	require.ErrorAs(t, pageSpec.Validate(page{Days: 5, Keep: 30, On: true, Level: "info"}), &bad)

	assert.Equal(t, map[string]string{"keep_days": "cannot exceed days"}, bad.Fields)
}

func TestDiffKeepsOnlyWhatChanged(t *testing.T) {
	next := yaml
	next.On = false
	next.Level = "debug"

	assert.Equal(t, map[string]any{"on": false, "level": "debug"}, pageSpec.Diff(yaml, next))
	assert.Empty(t, pageSpec.Diff(yaml, yaml))
}

func TestResolveLayersOverridesOnTheDefaults(t *testing.T) {
	log := zap.NewNop()

	t.Run("nothing saved is yaml", func(t *testing.T) {
		assert.Equal(t, yaml, pageSpec.Resolve(yaml, nil, log))
	})
	t.Run("an override wins and the rest stays yaml", func(t *testing.T) {
		want := yaml
		want.Days, want.On = 45, false
		assert.Equal(t, want, pageSpec.Resolve(yaml, []byte(`{"days":45,"on":false}`), log))
	})
	t.Run("an invalid or mistyped override falls back for that field only", func(t *testing.T) {
		want := yaml
		want.Level = "debug"
		got := pageSpec.Resolve(yaml, []byte(`{"days":0,"on":"yes","level":"debug"}`), log)
		assert.Equal(t, want, got)
	})
	t.Run("unknown keys and unreadable data never stop a start", func(t *testing.T) {
		assert.Equal(t, yaml, pageSpec.Resolve(yaml, []byte(`{"retired":1}`), log))
		assert.Equal(t, yaml, pageSpec.Resolve(yaml, []byte(`not json`), log))
	})
	t.Run("overrides that contradict each other are dropped together", func(t *testing.T) {
		assert.Equal(t, yaml, pageSpec.Resolve(yaml, []byte(`{"days":5}`), log), "keep_days 7 > days 5")
	})
}

type memory struct {
	rows map[string][]byte
	fail error
}

func (m *memory) GetSettingOverrides(scope string) ([]byte, error) { return m.rows[scope], m.fail }
func (m *memory) SaveSettingOverrides(scope string, data []byte) error {
	m.rows[scope] = data
	return m.fail
}
func (m *memory) DeleteSettingOverrides(scope string) error {
	delete(m.rows, scope)
	return m.fail
}

func newPage(t *testing.T) (*Registry, *Handle[page], *memory) {
	t.Helper()
	liveSeen, liveErr = nil, nil
	store := &memory{rows: map[string][]byte{}}
	reg := NewRegistry(store)
	h, err := Register(reg, "page", pageSpec, yaml)
	require.NoError(t, err)
	return reg, h, store
}

func TestRegisterRefusesBadDefaultsAndTwins(t *testing.T) {
	reg, _, _ := newPage(t)

	_, err := Register(reg, "page", pageSpec, yaml)
	require.ErrorContains(t, err, "registered twice")

	var bad *Error
	_, err = Register(reg, "other", pageSpec, page{})
	require.ErrorAs(t, err, &bad)
}

// A restart-only field is stored at once and changes nothing in the running gateway until the next
// start, and the answer says it is waiting.
func TestSavedRestartFieldsWaitForTheNextStart(t *testing.T) {
	_, h, store := newPage(t)
	next := yaml
	next.Days = 60

	view, err := h.Save(next)

	require.NoError(t, err)
	assert.Equal(t, next, view.Settings)
	assert.Equal(t, yaml, view.Defaults)
	assert.Equal(t, []string{"days"}, view.RestartRequired)
	assert.Empty(t, view.Applied)
	assert.JSONEq(t, `{"days":60}`, string(store.rows["page"]))
	assert.Equal(t, yaml, h.Running(), "the running gateway is not touched")
	assert.Empty(t, liveSeen)

	saved, err := h.Saved()
	require.NoError(t, err)
	assert.Equal(t, []string{"days"}, saved.RestartRequired, "a later read still shows what is pending")

	restarted := NewRegistry(store)
	again, err := Register(restarted, "page", pageSpec, yaml)
	require.NoError(t, err)
	assert.Equal(t, next, again.Running(), "the next start uses it")
}

// A live field is applied to the running gateway as part of the save, and is no longer pending.
func TestSavedLiveFieldsApplyAtOnce(t *testing.T) {
	_, h, _ := newPage(t)
	next := yaml
	next.Level = "debug"
	next.Days = 60

	view, err := h.Save(next)

	require.NoError(t, err)
	assert.Equal(t, []string{"level"}, view.Applied)
	assert.Equal(t, []string{"days"}, view.RestartRequired, "only the restart field still waits")
	assert.Equal(t, []string{"debug"}, liveSeen)
	running := h.Running()
	assert.Equal(t, "debug", running.Level, "the live field is in effect")
	assert.Equal(t, yaml.Days, running.Days, "the restart field is not")
}

func TestResetAppliesLiveFieldsBackAtOnce(t *testing.T) {
	_, h, store := newPage(t)
	next := yaml
	next.Level = "debug"
	_, err := h.Save(next)
	require.NoError(t, err)
	liveSeen = nil

	view, err := h.Reset()

	require.NoError(t, err)
	assert.Equal(t, yaml, view.Settings)
	assert.Equal(t, []string{"level"}, view.Applied)
	assert.Equal(t, []string{"info"}, liveSeen)
	assert.Empty(t, view.RestartRequired)
	assert.Empty(t, store.rows)
}

// The value is saved even when applying it fails; it then waits for a restart like any other.
func TestFailedLiveApplyIsReportedAndStaysPending(t *testing.T) {
	_, h, store := newPage(t)
	liveErr = errors.New("logger is closed")
	next := yaml
	next.Level = "debug"

	view, err := h.Save(next)

	require.ErrorIs(t, err, ErrNotApplied)
	assert.ErrorContains(t, err, "logger is closed")
	assert.JSONEq(t, `{"level":"debug"}`, string(store.rows["page"]), "still saved")
	assert.Equal(t, []string{"level"}, view.RestartRequired)
	assert.Equal(t, yaml, h.Running())
}

func TestSavingTheDefaultsRemovesTheOverrides(t *testing.T) {
	_, h, store := newPage(t)
	next := yaml
	next.Days = 60
	_, err := h.Save(next)
	require.NoError(t, err)

	_, err = h.Save(yaml)

	require.NoError(t, err)
	assert.Empty(t, store.rows)
}

func TestSaveRejectsABadFormAndStoresNothing(t *testing.T) {
	_, h, store := newPage(t)

	_, err := h.Save(page{Days: 0})

	var bad *Error
	require.ErrorAs(t, err, &bad)
	assert.Empty(t, store.rows)
	assert.Empty(t, liveSeen)
}

func TestStoreFailuresSurface(t *testing.T) {
	_, h, store := newPage(t)
	store.fail = errors.New("disk full")

	_, err := h.Save(page{Days: 31, Keep: 7, Level: "info"})
	require.ErrorContains(t, err, "disk full")
	_, err = h.Saved()
	require.ErrorContains(t, err, "disk full")
}

func TestRegistryServesPagesByName(t *testing.T) {
	reg, _, store := newPage(t)

	view, err := reg.View("page")
	require.NoError(t, err)
	assert.Equal(t, View[page]{Settings: yaml, Defaults: yaml, RestartRequired: []string{}}, view)

	saved, err := reg.Save("page", []byte(`{"days":60,"keep_days":7,"on":true,"level":"info"}`))
	require.NoError(t, err)
	assert.Equal(t, 60, int(saved.(View[page]).Settings.Days))
	assert.JSONEq(t, `{"days":60}`, string(store.rows["page"]))

	_, err = reg.Reset("page")
	require.NoError(t, err)
	assert.Empty(t, store.rows)

	for _, call := range []func() error{
		func() error { _, err := reg.View("nope"); return err },
		func() error { _, err := reg.Save("nope", []byte(`{}`)); return err },
		func() error { _, err := reg.Reset("nope"); return err },
	} {
		assert.ErrorIs(t, call(), ErrUnknownScope)
	}
}

// A form must carry every field: leaving one out must not quietly turn it off.
func TestRegistrySaveNeedsTheWholeForm(t *testing.T) {
	reg, _, store := newPage(t)

	_, err := reg.Save("page", []byte(`{"days":60}`))
	var bad *Error
	require.ErrorAs(t, err, &bad)
	assert.Equal(t, map[string]string{"keep_days": "is required", "on": "is required", "level": "is required"}, bad.Fields)

	_, err = reg.Save("page", []byte(`not json`))
	assert.ErrorIs(t, err, ErrBadForm)
	assert.Empty(t, store.rows)
}
