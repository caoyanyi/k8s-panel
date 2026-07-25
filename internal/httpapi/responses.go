package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

type fieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type errorBody struct {
	Code      string       `json:"code"`
	Message   string       `json:"message"`
	Details   []fieldError `json:"details,omitempty"`
	RequestID string       `json:"request_id"`
}

func writeData(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, map[string]any{"data": data})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var validationErr *domain.ValidationError
	switch {
	case errors.As(err, &validationErr):
		writeErrorStatus(w, r, http.StatusUnprocessableEntity, "validation_error", "请求参数不合法", []fieldError{{
			Field: validationErr.Field, Message: validationErr.Message,
		}})
	case errors.Is(err, domain.ErrUnauthorized):
		writeErrorStatus(w, r, http.StatusUnauthorized, "unauthorized", "请先登录或重新登录", nil)
	case errors.Is(err, domain.ErrForbidden):
		writeErrorStatus(w, r, http.StatusForbidden, "forbidden", "没有执行该操作的权限", nil)
	case errors.Is(err, domain.ErrNotFound):
		writeErrorStatus(w, r, http.StatusNotFound, "not_found", "目标资源不存在", nil)
	case errors.Is(err, domain.ErrConflict):
		writeErrorStatus(w, r, http.StatusConflict, "conflict", "资源名称或状态发生冲突", nil)
	case errors.Is(err, domain.ErrInvalidState):
		writeErrorStatus(w, r, http.StatusConflict, "invalid_state", "资源当前状态不允许该操作", nil)
	case errors.Is(err, domain.ErrBusy):
		writeErrorStatus(w, r, http.StatusServiceUnavailable, "server_busy", "服务繁忙，请稍后重试", nil)
	case errors.Is(err, domain.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		writeErrorStatus(w, r, http.StatusGatewayTimeout, "upstream_timeout", "上游请求超时", nil)
	case errors.Is(err, domain.ErrUpstream):
		writeErrorStatus(w, r, http.StatusBadGateway, "upstream_unavailable", "上游服务不可用或拒绝了请求", nil)
	default:
		writeErrorStatus(w, r, http.StatusInternalServerError, "internal_error", "服务处理请求时发生错误", nil)
	}
}

func writeInvalidJSON(w http.ResponseWriter, r *http.Request) {
	writeErrorStatus(w, r, http.StatusBadRequest, "invalid_json", "请求正文不是有效的 JSON", nil)
}

func writeErrorStatus(w http.ResponseWriter, r *http.Request, status int, code, message string, details []fieldError) {
	requestID, _ := r.Context().Value(requestIDKey).(string)
	writeJSON(w, status, map[string]any{"error": errorBody{
		Code: code, Message: message, Details: details, RequestID: requestID,
	}})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("request content type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}
