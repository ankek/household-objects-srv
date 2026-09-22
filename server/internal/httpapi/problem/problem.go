package problem

import (
	"encoding/json"
	"github.com/ankek/Household-Objects-Dev/server/internal/httpapi/requestid"
	"net/http"
)

const MediaType = "application/problem+json"

const typePrefix = "urn:hho:problem:"

type Problem struct {
	Type string `json:"type"`

	Title string `json:"title"`

	Status int `json:"status"`

	Detail string `json:"detail,omitempty"`

	RequestID string `json:"request_id,omitempty"`
}

func New(status int, kind, title string) Problem {
	return Problem{Type: typePrefix + kind, Title: title, Status: status}
}

func (p Problem) WithDetail(detail string) Problem {
	p.Detail = detail
	return p
}

func NotFound() Problem {
	return New(http.StatusNotFound, "not-found", "Not Found")
}

func Unauthorized() Problem {
	return New(http.StatusUnauthorized, "unauthorized", "Unauthorized")
}

func Forbidden() Problem {
	return New(http.StatusForbidden, "forbidden", "Forbidden")
}

func BadRequest(detail string) Problem {
	return New(http.StatusBadRequest, "bad-request", "Bad Request").WithDetail(detail)
}

func Conflict() Problem {
	return New(http.StatusConflict, "conflict", "Conflict")
}

func TooManyRequests() Problem {
	return New(http.StatusTooManyRequests, "too-many-requests", "Too Many Requests")
}

func MethodNotAllowed() Problem {
	return New(http.StatusMethodNotAllowed, "method-not-allowed", "Method Not Allowed")
}

func ContentTooLarge() Problem {
	return New(http.StatusRequestEntityTooLarge, "content-too-large", "Content Too Large")
}

func NotImplemented() Problem {
	return New(http.StatusNotImplemented, "not-implemented", "Not Implemented")
}

func Internal() Problem {
	return New(http.StatusInternalServerError, "internal", "Internal Server Error")
}

type UpgradeRequiredProblem struct {
	Problem
	MinimumVersion string `json:"minimum_version"`
}

func UpgradeRequired(minimum string) UpgradeRequiredProblem {
	return UpgradeRequiredProblem{
		Problem:        New(http.StatusUpgradeRequired, "upgrade-required", "Upgrade Required"),
		MinimumVersion: minimum,
	}
}

func WriteUpgradeRequired(w http.ResponseWriter, r *http.Request, p UpgradeRequiredProblem) {
	if p.Status != http.StatusUpgradeRequired {
		Write(w, r, Internal())
		return
	}
	if r != nil {
		p.RequestID = requestid.FromContext(r.Context())
	}

	body, err := json.Marshal(p)
	if err != nil {
		Write(w, r, Internal())
		return
	}

	h := w.Header()
	h.Set("Content-Type", MediaType)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(p.Status)
	_, _ = w.Write(body)
}

const fallbackBody = `{"type":"` + typePrefix + `internal","title":"Internal Server Error","status":500}`

func Write(w http.ResponseWriter, r *http.Request, p Problem) {
	if p.Status < 100 || p.Status > 599 {
		p = Internal()
	}

	if r != nil {
		p.RequestID = requestid.FromContext(r.Context())
	}

	body, err := json.Marshal(p)
	if err != nil {
		body, p.Status = []byte(fallbackBody), http.StatusInternalServerError
	}

	h := w.Header()
	h.Set("Content-Type", MediaType)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")

	w.WriteHeader(p.Status)
	_, _ = w.Write(body)
}
