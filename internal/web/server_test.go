package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zoomacode/homedash/internal/state"
)

func TestIndex_RendersClock(t *testing.T) {
	srv := New(state.New())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.Handler().ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status = %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `id="clock"`) {
		t.Errorf("body missing clock section")
	}
}
