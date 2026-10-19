package hostapi

import (
	"encoding/json"
	"fmt"
)

// Caller invokes a host RPC method and returns its raw JSON envelope.
type Caller func(method string, payload []byte) ([]byte, error)

// Client performs upstream HTTP calls through the host, so global proxy and
// TLS policy stay in one place.
type Client struct {
	call       Caller
	callbackID string
}

// Response is the non-streaming host HTTP response.
type Response struct {
	StatusCode int                 `json:"StatusCode"`
	Headers    map[string][]string `json:"Headers"`
	Body       []byte              `json:"Body"`
}

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// New creates a client bound to one plugin callback context.
func New(call Caller, callbackID string) *Client {
	return &Client{call: call, callbackID: callbackID}
}

// Do sends a non-streaming HTTP request through the host.
func (c *Client) Do(method, url string, headers map[string]string, body []byte) (*Response, error) {
	if c == nil || c.call == nil {
		return nil, fmt.Errorf("host call is unavailable")
	}
	req := struct {
		HostCallbackID string              `json:"host_callback_id,omitempty"`
		Method         string              `json:"method"`
		URL            string              `json:"url"`
		Headers        map[string][]string `json:"headers,omitempty"`
		Body           []byte              `json:"body,omitempty"`
	}{
		HostCallbackID: c.callbackID,
		Method:         method,
		URL:            url,
		Headers:        normalizeHeaders(headers),
		Body:           body,
	}
	raw, errMarshal := json.Marshal(req)
	if errMarshal != nil {
		return nil, fmt.Errorf("marshal host http request: %w", errMarshal)
	}
	rawResp, errCall := c.call("host.http.do", raw)
	if errCall != nil {
		return nil, fmt.Errorf("host http call: %w", errCall)
	}
	var env envelope
	if errUnmarshal := json.Unmarshal(rawResp, &env); errUnmarshal != nil {
		return nil, fmt.Errorf("decode host http envelope: %w", errUnmarshal)
	}
	if !env.OK {
		if env.Error != nil && env.Error.Message != "" {
			return nil, fmt.Errorf("host http failed (%s): %s", env.Error.Code, env.Error.Message)
		}
		return nil, fmt.Errorf("host http failed")
	}
	var resp Response
	if errUnmarshal := json.Unmarshal(env.Result, &resp); errUnmarshal != nil {
		return nil, fmt.Errorf("decode host http response: %w", errUnmarshal)
	}
	return &resp, nil
}

func normalizeHeaders(headers map[string]string) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string][]string, len(headers))
	for key, value := range headers {
		if key == "" {
			continue
		}
		out[key] = []string{value}
	}
	return out
}
