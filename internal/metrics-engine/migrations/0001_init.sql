CREATE TABLE IF NOT EXISTS events (
    schema_version                INTEGER     NOT NULL,
    id                            VARCHAR     PRIMARY KEY,
    request_id                    VARCHAR,
    kind                          VARCHAR     NOT NULL,
    request_kind                  VARCHAR,
    request_endpoint              VARCHAR     NOT NULL,
    cost_nano_usd                 BIGINT      NOT NULL,

    governance_client_id          VARCHAR,
    governance_virtual_key_id     VARCHAR,
    governance_budget_id          VARCHAR,

    outcome_result                VARCHAR     NOT NULL,
    outcome_http_status           INTEGER     NOT NULL,
    outcome_upstream_http_status  INTEGER,
    outcome_error_code            VARCHAR,
    outcome_error_category        VARCHAR,
    outcome_error_message         VARCHAR,

    started_at                    TIMESTAMPTZ NOT NULL,
    timing_total_us               BIGINT      NOT NULL,
    timing_ttfb_us                BIGINT,
    timing_ttft_us                BIGINT,
    timing_stages                 JSON,

    routing_mode                  VARCHAR,
    routing_credential_id         VARCHAR,
    routing_attempts              INTEGER,
    routing_attempt_list          JSON,

    llm_requested_model           VARCHAR,
    llm_provider                  VARCHAR,
    llm_model                     VARCHAR,
    llm_stream                    BOOLEAN,
    llm_finish_reason             VARCHAR,
    llm_tool_calls                JSON,
    llm_params                    JSON,
    llm_pricing                   JSON,

    usage_tier                    VARCHAR,
    usage_input_tokens            BIGINT,
    usage_output_tokens           BIGINT,
    usage_total_tokens            BIGINT,
    usage_cached_input_tokens     BIGINT,
    usage_cache_creation_tokens   BIGINT,
    usage_reasoning_tokens        BIGINT,
    usage_detail                  JSON,

    headers                       JSON,
    payload_state                 VARCHAR     NOT NULL
);

CREATE TABLE IF NOT EXISTS event_payloads (
    id                   VARCHAR     PRIMARY KEY,
    started_at           TIMESTAMPTZ NOT NULL,
    client_request       JSON,
    normalized_request   JSON,
    provider_request     JSON,
    provider_response    JSON,
    normalized_response  JSON,
    client_response      JSON
);
