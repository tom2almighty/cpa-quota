package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"cpa-quota/internal/model"
)

// Handle serves the quota dashboard resource. All data comes from CPA's v8
// management API, so the page itself is just the embedded single-file app.
func Handle(html []byte, req model.ManagementRequest) model.ManagementResponse {
	if strings.HasPrefix(req.Path, "/v0/management/") {
		return jsonError(http.StatusNotFound, "not_found")
	}
	if req.Method != http.MethodGet {
		return jsonError(http.StatusMethodNotAllowed, "method_not_allowed")
	}
	headers := http.Header{}
	headers.Set("Content-Type", "text/html; charset=utf-8")
	headers.Set("Cache-Control", "no-cache")
	return model.ManagementResponse{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       html,
	}
}

func jsonError(status int, code string) model.ManagementResponse {
	body, _ := json.Marshal(map[string]string{"code": code})
	headers := http.Header{}
	headers.Set("Content-Type", "application/json; charset=utf-8")
	return model.ManagementResponse{StatusCode: status, Headers: headers, Body: body}
}
