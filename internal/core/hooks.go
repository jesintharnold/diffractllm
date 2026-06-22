package core

import (
	"time"

	"go.uber.org/zap"
)

type Hook interface {
	Name() string
	Execute(rctx *DiffractLLMContext) *DiffractLLMError
}

type HookStatus uint8

const (
	HookOK HookStatus = iota
	HookRejected
	HookFailed
)

func (hs HookStatus) String() string {
	switch hs {
	case HookOK:
		return "ok"
	case HookRejected:
		return "rejected"
	case HookFailed:
		return "failed"
	default:
		return "unknown"
	}
}

const maxHooks = 5

type HookResult struct {
	Name     string
	Status   HookStatus
	Duration time.Duration
	Error    string
}

type HookLog struct {
	PreCallResults      [maxHooks]HookResult
	PreProviderResults  [maxHooks]HookResult
	PostProviderResults [maxHooks]HookResult
	PostCallResults     [maxHooks]HookResult

	PreCallCount      int
	PreProviderCount  int
	PostProviderCount int
	PostCallCount     int

	PreCallTotal      time.Duration
	PreProviderTotal  time.Duration
	PostProviderTotal time.Duration
	PostCallTotal     time.Duration
}

func (hl *HookLog) reset() {
	hl.PreCallCount = 0
	hl.PreProviderCount = 0
	hl.PostProviderCount = 0
	hl.PostCallCount = 0
	hl.PreCallTotal = 0
	hl.PreProviderTotal = 0
	hl.PostProviderTotal = 0
	hl.PostCallTotal = 0
}

type HookEngine struct {
	preCallHooks      [maxHooks]Hook
	preProviderHooks  [maxHooks]Hook
	postProviderHooks [maxHooks]Hook
	postCallHooks     [maxHooks]Hook
	preCallCount      int
	preProviderCount  int
	postProviderCount int
	postCallCount     int
	logger            *zap.Logger
}

func NewHookEngine(logger *zap.Logger) *HookEngine {
	return &HookEngine{
		logger: logger.With(zap.String("component", "hooks")),
	}
}

func (he *HookEngine) AddPreCallHook(hook Hook) error {
	if he.preCallCount >= maxHooks {
		return NewInternalError("hooks", "pre-call hook limit reached", nil)
	}
	he.preCallHooks[he.preCallCount] = hook
	he.preCallCount++
	he.logger.Info("pre-call hook registered", zap.String("name", hook.Name()))
	return nil
}

func (he *HookEngine) AddPreProviderHook(hook Hook) error {
	if he.preProviderCount >= maxHooks {
		return NewInternalError("hooks", "pre-provider hook limit reached", nil)
	}
	he.preProviderHooks[he.preProviderCount] = hook
	he.preProviderCount++
	he.logger.Info("pre-provider hook registered", zap.String("name", hook.Name()))
	return nil
}

func (he *HookEngine) AddPostProviderHook(hook Hook) error {
	if he.postProviderCount >= maxHooks {
		return NewInternalError("hooks", "post-provider hook limit reached", nil)
	}
	he.postProviderHooks[he.postProviderCount] = hook
	he.postProviderCount++
	he.logger.Info("post-provider hook registered", zap.String("name", hook.Name()))
	return nil
}

func (he *HookEngine) AddPostCallHook(hook Hook) error {
	if he.postCallCount >= maxHooks {
		return NewInternalError("hooks", "post-call hook limit reached", nil)
	}
	he.postCallHooks[he.postCallCount] = hook
	he.postCallCount++
	he.logger.Info("post-call hook registered", zap.String("name", hook.Name()))
	return nil
}

func (he *HookEngine) RunPreCallHooks(rctx *DiffractLLMContext) *DiffractLLMError {
	log := &rctx.HookLog

	for i := 0; i < he.preCallCount; i++ {
		hook := he.preCallHooks[i]
		start := time.Now()
		hookErr := hook.Execute(rctx)
		elapsed := time.Since(start)

		result := &log.PreCallResults[log.PreCallCount]
		result.Name = hook.Name()
		result.Duration = elapsed

		log.PreCallCount++
		log.PreCallTotal += elapsed

		if hookErr != nil {
			result.Status = HookRejected
			result.Error = hookErr.Message
			he.logger.Warn("pre-call hook rejected", zap.String("hook", hook.Name()), zap.Duration("duration", elapsed), zap.String("error", hookErr.Message))
			return hookErr
		}
		result.Status = HookOK
	}

	return nil
}

func (he *HookEngine) RunPreProviderHooks(rctx *DiffractLLMContext) *DiffractLLMError {
	log := &rctx.HookLog
	for i := 0; i < he.preProviderCount; i++ {
		hook := he.preProviderHooks[i]
		start := time.Now()
		hookErr := hook.Execute(rctx)
		elapsed := time.Since(start)

		result := &log.PreProviderResults[log.PreProviderCount]
		result.Name = hook.Name()
		result.Duration = elapsed

		log.PreProviderCount++
		log.PreProviderTotal += elapsed

		if hookErr != nil {
			result.Status = HookRejected
			result.Error = hookErr.Message
			he.logger.Warn("pre-provider hook rejected", zap.String("hook", hook.Name()), zap.Duration("duration", elapsed), zap.String("error", hookErr.Message))
			return hookErr
		}
		result.Status = HookOK
	}
	return nil
}

// Important thing here is post hooks can only log , can't response to client
func (he *HookEngine) RunPostProviderHooks(rctx *DiffractLLMContext) {
	log := &rctx.HookLog
	for i := 0; i < he.postProviderCount; i++ {
		hook := he.postProviderHooks[i]
		start := time.Now()
		hookErr := hook.Execute(rctx)
		elapsed := time.Since(start)

		result := &log.PostProviderResults[log.PostProviderCount]
		result.Name = hook.Name()
		result.Duration = elapsed

		log.PostProviderCount++
		log.PostProviderTotal += elapsed

		if hookErr != nil {
			result.Status = HookFailed
			result.Error = hookErr.Message
			he.logger.Error("post-provider hook failed",
				zap.String("hook", hook.Name()),
				zap.Duration("duration", elapsed),
				zap.String("error", hookErr.Message))
		} else {
			result.Status = HookOK
		}
	}
}

func (he *HookEngine) RunPostCallHooks(rctx *DiffractLLMContext) {
	log := &rctx.HookLog
	for i := 0; i < he.postCallCount; i++ {
		hook := he.postCallHooks[i]
		start := time.Now()
		hookErr := hook.Execute(rctx)
		elapsed := time.Since(start)

		result := &log.PostCallResults[log.PostCallCount]
		result.Name = hook.Name()
		result.Duration = elapsed

		log.PostCallCount++
		log.PostCallTotal += elapsed

		if hookErr != nil {
			result.Status = HookFailed
			result.Error = hookErr.Message
			he.logger.Warn("post-call hook failed", zap.String("hook", hook.Name()), zap.Duration("duration", elapsed), zap.String("error", hookErr.Message))
		} else {
			result.Status = HookOK
		}
	}
}
