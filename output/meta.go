package output

// Envelope is the stable {data, meta} machine shape rungrad emits under
// --include-meta. The command's normal result stays under data; response
// metadata is exposed separately under meta.
type Envelope struct {
	Data any  `json:"data"`
	Meta Meta `json:"meta"`
}

// Meta is rungrad's generic response-metadata view. Every field is optional and
// omitted from JSON when empty, so a command that attaches no metadata still
// produces a deterministic empty object ({}) under --include-meta. Keep secrets
// out of metadata; it is emitted verbatim in machine output.
type Meta struct {
	RequestID   string         `json:"request_id,omitempty"`
	RequestIDs  []string       `json:"request_ids,omitempty"`
	Pagination  *Pagination    `json:"pagination,omitempty"`
	RateLimit   *RateLimit     `json:"rate_limit,omitempty"`
	Retry       *Retry         `json:"retry,omitempty"`
	Idempotency *Idempotency   `json:"idempotency,omitempty"`
	Extra       map[string]any `json:"extra,omitempty"`
}

// Pagination describes page-number or cursor pagination. Pointers distinguish a
// real zero or false from an absent value, so empty pages and final cursor pages
// can still be reported precisely.
type Pagination struct {
	Page       *int   `json:"page,omitempty"`
	PerPage    *int   `json:"per_page,omitempty"`
	TotalPages *int   `json:"total_pages,omitempty"`
	TotalItems *int   `json:"total_items,omitempty"`
	NextCursor string `json:"next_cursor,omitempty"`
	PrevCursor string `json:"prev_cursor,omitempty"`
	HasMore    *bool  `json:"has_more,omitempty"`
}

// RateLimit preserves parsed and raw rate-limit values. Pointers distinguish a
// real zero from an absent value; Raw keeps original header text for diagnostics.
type RateLimit struct {
	Limit     *int64            `json:"limit,omitempty"`
	Remaining *int64            `json:"remaining,omitempty"`
	Reset     *int64            `json:"reset,omitempty"`
	Raw       map[string]string `json:"raw,omitempty"`
}

// Retry reports physical request attempts and the waits between them in
// milliseconds.
type Retry struct {
	Attempts int     `json:"attempts,omitempty"`
	WaitsMS  []int64 `json:"waits_ms,omitempty"`
}

// Idempotency reports idempotency state. Replayed is a pointer so an explicit
// false (the backend reported a non-replayed request) is distinguishable from
// "no replay state was reported".
type Idempotency struct {
	Key      string `json:"key,omitempty"`
	Replayed *bool  `json:"replayed,omitempty"`
}
