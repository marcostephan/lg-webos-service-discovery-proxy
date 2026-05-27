package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestHandler(t *testing.T) {
	cases := []struct {
		name, method, path string
		wantStatus         int
		wantTimeHeader     bool
	}{
		{"initservices v7", "POST", "/rest/sdp/v7.0/initservices", 200, true},
		{"initservices v14", "POST", "/rest/sdp/v14.0/initservices", 200, true},
		{"initservices wrong method", "GET", "/rest/sdp/v14.0/initservices", 404, false},
		{"notice", "GET", "/rest/sdp/v14.0/notice", 200, false},
		{"eula", "GET", "/rest/sdp/v14.0/eula", 200, false},
		{"server status", "GET", "/rest/apps/webos8.0/serverstatus/status", 200, false},
		{"unknown", "GET", "/something/else", 404, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status: got %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantTimeHeader && rec.Header().Get("X-Server-Time") == "" {
				t.Fatal("missing X-Server-Time header")
			}
		})
	}
}

func TestInitServicesReplyIsValid(t *testing.T) {
	if len(initServicesReply) == 0 {
		t.Fatal("initservices reply is empty")
	}
	if !json.Valid(initServicesReply) {
		t.Fatal("initservices reply is not valid JSON")
	}
}
