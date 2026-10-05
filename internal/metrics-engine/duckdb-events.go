package metricsengine

import (
	"database/sql/driver"
	"encoding/json"
	"errors"

	"github.com/bytedance/sonic"
)

const (
	colSchemaVersion = iota
	colID
	colRequestID
	colKind
	colRequestKind
	colRequestEndpoint
	colCostNanoUSD
	colGovernanceClientID
	colGovernanceVirtualKeyID
	colGovernanceBudgetID
	colOutcomeResult
	colOutcomeHTTPStatus
	colOutcomeUpstreamHTTPStatus
	colOutcomeErrorCode
	colOutcomeErrorCategory
	colOutcomeErrorMessage
	colStartedAt
	colTimingTotalUS
	colTimingTTFBUS
	colTimingTTFTUS
	colTimingStages
	colRoutingMode
	colRoutingCredentialID
	colRoutingCredentialName
	colRoutingAttempts
	colRoutingAttemptList
	colLLMRequestedModel
	colLLMProvider
	colLLMModel
	colLLMUpstreamModel
	colLLMStream
	colLLMFinishReason
	colLLMToolCalls
	colLLMParams
	colLLMPricing
	colUsageTier
	colUsageInputTokens
	colUsageOutputTokens
	colUsageTotalTokens
	colUsageCachedInputTokens
	colUsageCacheCreationTokens
	colUsageReasoningTokens
	colUsageDetail
	colHeaders
	colPayloadState
	numEventCols
)

var eventColumnNames = [numEventCols]string{
	colSchemaVersion:             "schema_version",
	colID:                        "id",
	colRequestID:                 "request_id",
	colKind:                      "kind",
	colRequestKind:               "request_kind",
	colRequestEndpoint:           "request_endpoint",
	colCostNanoUSD:               "cost_nano_usd",
	colGovernanceClientID:        "governance_client_id",
	colGovernanceVirtualKeyID:    "governance_virtual_key_id",
	colGovernanceBudgetID:        "governance_budget_id",
	colOutcomeResult:             "outcome_result",
	colOutcomeHTTPStatus:         "outcome_http_status",
	colOutcomeUpstreamHTTPStatus: "outcome_upstream_http_status",
	colOutcomeErrorCode:          "outcome_error_code",
	colOutcomeErrorCategory:      "outcome_error_category",
	colOutcomeErrorMessage:       "outcome_error_message",
	colStartedAt:                 "started_at",
	colTimingTotalUS:             "timing_total_us",
	colTimingTTFBUS:              "timing_ttfb_us",
	colTimingTTFTUS:              "timing_ttft_us",
	colTimingStages:              "timing_stages",
	colRoutingMode:               "routing_mode",
	colRoutingCredentialID:       "routing_credential_id",
	colRoutingCredentialName:     "routing_credential_name",
	colRoutingAttempts:           "routing_attempts",
	colRoutingAttemptList:        "routing_attempt_list",
	colLLMRequestedModel:         "llm_requested_model",
	colLLMProvider:               "llm_provider",
	colLLMModel:                  "llm_model",
	colLLMUpstreamModel:          "llm_upstream_model",
	colLLMStream:                 "llm_stream",
	colLLMFinishReason:           "llm_finish_reason",
	colLLMToolCalls:              "llm_tool_calls",
	colLLMParams:                 "llm_params",
	colLLMPricing:                "llm_pricing",
	colUsageTier:                 "usage_tier",
	colUsageInputTokens:          "usage_input_tokens",
	colUsageOutputTokens:         "usage_output_tokens",
	colUsageTotalTokens:          "usage_total_tokens",
	colUsageCachedInputTokens:    "usage_cached_input_tokens",
	colUsageCacheCreationTokens:  "usage_cache_creation_tokens",
	colUsageReasoningTokens:      "usage_reasoning_tokens",
	colUsageDetail:               "usage_detail",
	colHeaders:                   "headers",
	colPayloadState:              "payload_state",
}

var eventPayloadColNames = []string{
	"id",
	"started_at",
	"client_request",
	"normalized_request",
	"provider_request",
	"provider_response",
	"normalized_response",
	"client_response",
}

func (e *Event) toAppenderValues(dst []driver.Value) error {
	var errs []error
	js := func(v any) driver.Value {
		b, err := sonic.Marshal(v)
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		if s := string(b); s == "null" || s == "[]" || s == "{}" {
			return nil
		}
		return json.RawMessage(b) // JSON columns json.Marshal their value: raw, not a string
	}

	clear(dst)

	dst[colSchemaVersion] = e.SchemaVersion
	dst[colID] = e.ID
	dst[colRequestID] = nullCheck(e.RequestID)

	dst[colKind] = string(e.Kind)
	dst[colRequestKind] = nullCheck(string(e.RequestKind))
	dst[colRequestEndpoint] = e.RequestEndpoint

	dst[colCostNanoUSD] = e.CostNanoUSD

	dst[colGovernanceClientID] = nullCheck(e.Governance.ClientID)
	dst[colGovernanceVirtualKeyID] = nullCheck(e.Governance.VirtualKeyID)
	dst[colGovernanceBudgetID] = nullCheck(e.Governance.BudgetID)

	dst[colOutcomeResult] = string(e.Outcome.Result)
	dst[colOutcomeHTTPStatus] = e.Outcome.HTTPStatus
	dst[colOutcomeUpstreamHTTPStatus] = nz(e.Outcome.UpstreamHTTPStatus)
	dst[colOutcomeErrorCode] = nullCheck(e.Outcome.ErrorCode)
	dst[colOutcomeErrorCategory] = nullCheck(e.Outcome.ErrorCategory)
	dst[colOutcomeErrorMessage] = nullCheck(e.Outcome.ErrorMessage)

	dst[colStartedAt] = e.Timing.Start
	dst[colTimingTotalUS] = e.Timing.TotalUS
	dst[colTimingTTFBUS] = nz(e.Timing.TTFBUS)
	dst[colTimingTTFTUS] = nz(e.Timing.TTFTUS)
	dst[colTimingStages] = js(e.Timing.Stages)

	if rt := e.Routing; rt != nil {
		dst[colRoutingMode] = rt.Mode.String()
		dst[colRoutingCredentialID] = nullCheck(rt.CredentialID)
		dst[colRoutingCredentialName] = nullCheck(rt.CredentialName)
		dst[colRoutingAttempts] = nz(rt.AttemptCount)
		dst[colRoutingAttemptList] = js(rt.Attempts)
	}

	if l := e.LLM; l != nil {
		dst[colLLMRequestedModel] = nullCheck(l.RequestedModel)
		dst[colLLMProvider] = nullCheck(l.Provider)
		dst[colLLMModel] = nullCheck(l.Model)
		dst[colLLMUpstreamModel] = nullCheck(l.UpstreamModel)
		dst[colLLMStream] = l.Stream
		dst[colLLMFinishReason] = nullCheck(l.FinishReason)
		dst[colLLMToolCalls] = js(l.ToolCalls)
		dst[colLLMParams] = rawJSON(l.Params)
		dst[colLLMPricing] = js(l.Pricing)

		if u := l.Usage; u != nil {
			dst[colUsageTier] = u.Tier.String()
			dst[colUsageInputTokens] = nz(u.InputTokens)
			dst[colUsageOutputTokens] = nz(u.OutputTokens)
			dst[colUsageTotalTokens] = nz(u.TotalTokens)
			dst[colUsageCachedInputTokens] = nz(u.CachedInputTokens)
			dst[colUsageCacheCreationTokens] = nz(u.CacheCreationTokens)
			dst[colUsageReasoningTokens] = nz(u.ReasoningTokens)
			dst[colUsageDetail] = js(u)
		}
	}

	dst[colHeaders] = js(e.Headers)
	dst[colPayloadState] = string(e.PayloadState)

	return errors.Join(errs...)
}

func (e *Event) toPayloadValues() []driver.Value {
	p := e.Payload
	return []driver.Value{
		e.ID,
		e.Timing.Start,
		bodyJSON(p.ClientRequest),
		bodyJSON(p.NormalizedRequest),
		bodyJSON(p.ProviderRequest),
		bodyJSON(p.ProviderResponse),
		bodyJSON(p.NormalizedResponse),
		bodyJSON(p.ClientResponse),
	}
}
