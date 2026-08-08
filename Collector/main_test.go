package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParserDoesNotRetainSensitiveLine(t *testing.T) {
	const secret = "very-secret-account-token"
	item, ok := parseGameLine("ACClient: Online UserId=123 --gc_token " + secret)
	if !ok || item.Type != "ac_state" || item.State != "online" {
		t.Fatalf("unexpected event: %#v", item)
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(secret)) || bytes.Contains(encoded, []byte("123")) {
		t.Fatalf("normalized event leaked source data: %s", encoded)
	}
}

func TestParserPreservesGameTimestamp(t *testing.T) {
	item, ok := parseGameLine("[2026.08.07-11.07.59:178][578] ACClient: Online")
	if !ok {
		t.Fatal("expected parsed event")
	}
	if item.At != "2026-08-07T11:07:59.178Z" {
		t.Fatalf("timestamp = %q", item.At)
	}
}

func TestParserCapturesDiagnosticsWithoutPayloads(t *testing.T) {
	const secret = "opaque-mrac-payload-must-not-leave-device"
	tests := []struct {
		line      string
		eventType string
		state     string
		name      string
		code      int64
		length    int64
	}{
		{
			`[2026.08.07-10.18.06:786][995] GLogBackendRpcWsCalls: Verbose: [RPC Call (48)] FMracServiceWs::ClientRequest : '{"value":"` + secret + `"}'`,
			"mrac", "call", "FMracServiceWs::ClientRequest", 48, 0,
		},
		{
			`[2026.08.07-10.18.06:886][0] GLogBackendRpcWsCalls: Verbose: [RPC Response (48), traceparent: redacted] FMracServiceWs::ClientRequest : '{}'`,
			"mrac", "response", "FMracServiceWs::ClientRequest", 48, 0,
		},
		{
			`[2026.08.07-10.18.06:886][0] ACClient: Verbose: Received server response (681 bytes)`,
			"mrac", "client_response", "", 0, 681,
		},
		{
			`[2026.08.07-10.18.01:283][670] GLogBackendRpcWsCalls: Verbose: [RPC Call (1)] FAuthenticationServiceWs::PlatformAuth : '{}'`,
			"rpc", "call", "FAuthenticationServiceWs::PlatformAuth", 1, 0,
		},
		{
			`[2026.08.07-10.18.30:253][669] GLogBackendRpcWsCalls: Verbose: [RPC Response (2)] FSessionProviderServiceWs::Ping : '{}'`,
			"rpc", "response", "FSessionProviderServiceWs::Ping", 2, 0,
		},
		{
			`[2026.08.07-10.18.01:253][669] GLogBackendRpcWs: Connection to wss://backend.example/server established`,
			"backend_state", "connected", "", 0, 0,
		},
		{
			`[2026.08.07-10.52.15:344][845] Log file closed, 08/07/26 12:52:15`,
			"lifecycle", "log_closed", "", 0, 0,
		},
	}
	for _, test := range tests {
		item, ok := parseGameLine(test.line)
		if !ok || item.Type != test.eventType || item.State != test.state || item.Name != test.name || item.Code != test.code || item.Length != test.length {
			t.Errorf("parseGameLine(%q) = %#v, %v", test.line, item, ok)
		}
		encoded, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(encoded, []byte(secret)) || bytes.Contains(encoded, []byte("backend.example")) {
			t.Fatalf("normalized diagnostic leaked source payload: %s", encoded)
		}
	}
	if item, ok := parseGameLine("ACClient: Verbose: OnLoginComplete UserId=secret"); ok {
		t.Fatalf("unstructured ACClient line became an event: %#v", item)
	}
}

func TestRunSummaryIncludesCaptureBounds(t *testing.T) {
	start := "2026-08-07T10:18:00Z"
	end := "2026-08-07T10:19:04Z"
	events := []storedEvent{
		{Event: event{At: start, Type: "session_start", State: "recent_log"}},
		{Event: event{At: "2026-08-07T10:18:01Z", Type: "diagnostic", Name: "GLogBackendRpcWsCalls", State: "veryverbose"}},
		{Event: event{At: "2026-08-07T10:18:06Z", Type: "mrac", State: "call"}},
		{Event: event{At: "2026-08-07T10:18:07Z", Type: "mrac", State: "response"}},
		{Event: event{At: "2026-08-07T10:18:08Z", Type: "rpc", State: "call"}},
		{Event: event{At: "2026-08-07T10:18:09Z", Type: "rpc", State: "response"}},
		{Event: event{At: "2026-08-07T10:18:30Z", Type: "rpc", State: "call", Name: "FSessionProviderServiceWs::Ping"}},
		{Event: event{At: "2026-08-07T10:18:30.1Z", Type: "rpc", State: "response", Name: "FSessionProviderServiceWs::Ping"}},
		{Event: event{At: "2026-08-07T10:18:40Z", Type: "collector_state", State: "resumed", DurationMS: 5000}},
		{Event: event{At: "2026-08-07T10:19:03Z", Type: "backend_close", Code: 1006}},
		{Event: event{At: end, Type: "session_end", State: "process_exit"}},
	}
	summary := summarize(events)
	if summary.Started != start || summary.Ended != end || summary.Duration != "1m4s" {
		t.Fatalf("capture bounds: %#v", summary)
	}
	if !summary.Diagnostics || !summary.MRAC || summary.MRACState != "response" || summary.RPCCalls != 2 || summary.RPCResponses != 2 {
		t.Fatalf("diagnostic summary: %#v", summary)
	}
	if summary.PingCalls != 1 || summary.PingResponses != 1 || summary.PingToClose != "32.9s" {
		t.Fatalf("ping summary: %#v", summary)
	}
	if summary.CollectorGaps != 1 || summary.LongestGap != "5s" {
		t.Fatalf("collector gaps: %#v", summary)
	}
}

func TestCollectorGapIsExplicitAndBounded(t *testing.T) {
	start := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	if _, ok := collectorGap(start, start.Add(4*time.Second)); ok {
		t.Fatal("normal polling delay became a gap")
	}
	item, ok := collectorGap(start, start.Add(6*time.Second))
	if !ok || item.Type != "collector_state" || item.State != "resumed" || item.DurationMS != 6000 {
		t.Fatalf("collector gap = %#v, %v", item, ok)
	}
}

func TestProcessDiagnosticsRemainBounded(t *testing.T) {
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	present := true
	items := []event{
		{At: stamp, Type: "process_sample", State: "running", Threads: 64, FDs: 512, Sockets: 8},
		{At: stamp, Type: "env_flag", Name: "GC_PIPE_NAME", Value: &present},
		{At: stamp, Type: "process_runtime", Name: "steamrt", Version: "3"},
		{At: stamp, Type: "collector_heartbeat", State: "active"},
	}
	for _, item := range items {
		if err := validateEvent(&item); err != nil {
			t.Fatalf("valid process diagnostic rejected: %v", err)
		}
	}
	items[0].Threads = 1<<20 + 1
	if err := validateEvent(&items[0]); err == nil {
		t.Fatal("unbounded process diagnostic accepted")
	}
}

func TestIngestRequiresTokenAndStoresNormalizedEvent(t *testing.T) {
	directory := t.TempDir()
	server := &server{dataDir: directory, adminToken: strings.Repeat("b", 32)}
	code, _, err := server.createInvite("deck-a")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := server.enroll(code)
	if err != nil {
		t.Fatal(err)
	}
	input := envelope{
		Schema: schemaVersion, RunID: newRunID(), DeviceLabel: "deck-a",
		AgentVersion: "test", Mode: "baseline",
		Events: []event{{At: time.Now().UTC().Format(time.RFC3339Nano), Type: "ac_state", State: "online"}},
	}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+credential.Token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ingest(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	data, err := os.ReadFile(directory + "/" + input.RunID + ".jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"state":"online"`)) {
		t.Fatalf("event not stored: %s", data)
	}

	request = httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	server.ingest(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", response.Code)
	}
}

func TestEnrollmentIsOneTimeAndStoresOnlyHash(t *testing.T) {
	directory := t.TempDir()
	server := &server{dataDir: directory, adminToken: strings.Repeat("b", 32)}
	code, _, err := server.createInvite("volunteer-deck")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := server.enroll(code)
	if err != nil {
		t.Fatal(err)
	}
	if credential.DeviceLabel != "volunteer-deck" || len(credential.Token) != 64 {
		t.Fatalf("unexpected credential: %#v", credential)
	}
	if _, err := server.enroll(code); !errors.Is(err, errInvalidEnrollment) {
		t.Fatalf("second enrollment error = %v", err)
	}
	registry, err := os.ReadFile(directory + "/clients.json")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(registry, []byte(code)) || bytes.Contains(registry, []byte(credential.Token)) {
		t.Fatal("registry stored a plaintext secret")
	}
	if !server.authenticateClient(credential.Token, "volunteer-deck") {
		t.Fatal("issued token was not accepted")
	}
	if server.authenticateClient(credential.Token, "different-deck") {
		t.Fatal("token was accepted for another label")
	}
	newCode, _, err := server.createInvite("volunteer-deck")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := server.enroll(newCode)
	if err != nil {
		t.Fatal(err)
	}
	if server.authenticateClient(credential.Token, "volunteer-deck") {
		t.Fatal("old token survived re-enrollment")
	}
	if !server.authenticateClient(replacement.Token, "volunteer-deck") {
		t.Fatal("replacement token was not accepted")
	}
}

func TestPublicDownloadAndProtectedAdmin(t *testing.T) {
	server := &server{dataDir: t.TempDir(), adminToken: strings.Repeat("b", 32), publicURL: "https://collector.test"}

	response := httptest.NewRecorder()
	server.landing(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "/download/install.sh") {
		t.Fatalf("landing response: %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	server.installerHandler(response, httptest.NewRequest(http.MethodGet, "/download/install.sh", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "One-time enrollment code") {
		t.Fatalf("installer response: %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), `collector_url="https://collector.test"`) {
		t.Fatal("downloaded installer did not receive the configured public URL")
	}
	if !strings.Contains(response.Body.String(), "WRF COLLECTOR DIAGNOSTICS") || !strings.Contains(response.Body.String(), `"mode":"instrumented"`) {
		t.Fatal("downloaded installer did not enable instrumented diagnostics")
	}
	if !strings.Contains(response.Body.String(), "--connect-timeout 10 --max-time 120") || !strings.Contains(response.Body.String(), "Downloading collector agent...") {
		t.Fatal("downloaded installer did not expose bounded downloads")
	}

	response = httptest.NewRecorder()
	server.dashboard(response, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("admin status = %d", response.Code)
	}
}

func TestInviteCSRFToken(t *testing.T) {
	server := &server{dataDir: t.TempDir(), adminToken: strings.Repeat("b", 32)}
	request := func(token string) *http.Request {
		form := url.Values{"device_label": {"volunteer-deck"}, "csrf_token": {token}}
		result := httptest.NewRequest(http.MethodPost, "/admin/invites", strings.NewReader(form.Encode()))
		result.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		result.SetBasicAuth("admin", server.adminToken)
		return result
	}

	response := httptest.NewRecorder()
	server.createInviteHandler(response, request("wrong"))
	if response.Code != http.StatusForbidden {
		t.Fatalf("invalid CSRF token status = %d", response.Code)
	}

	response = httptest.NewRecorder()
	server.createInviteHandler(response, request(server.csrfToken()))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Code for volunteer-deck") {
		t.Fatalf("valid CSRF token response: %d %s", response.Code, response.Body.String())
	}
}

func TestRejectsUnstructuredEvent(t *testing.T) {
	input := envelope{
		Schema: schemaVersion, RunID: newRunID(), DeviceLabel: "deck-a",
		AgentVersion: "test", Mode: "baseline",
		Events: []event{{At: time.Now().UTC().Format(time.RFC3339Nano), Type: "raw_log", Name: "payload"}},
	}
	if err := validateEnvelope(&input); err == nil {
		t.Fatal("raw_log event was accepted")
	}
}

func TestEmbeddedUpdatePublicKey(t *testing.T) {
	payload := []byte("wrf-collector-signature-test-v1")
	manifest := updateManifest{
		SHA256:    "d0ad2ff71ec7771c506770cead184a45d30ba6fcefd3360544c706f697677b0d",
		Signature: "4fljX8ttzK0bD96MRu1R/r8Um6YBIMMnicYj86YHh8T/68WY14Ql5XmmRVO1+/ovloagOejyFJdox90NVrpQDQ==",
	}
	if err := verifyUpdate(payload, &manifest); err != nil {
		t.Fatal(err)
	}
	payload[0] ^= 1
	if err := verifyUpdate(payload, &manifest); err == nil {
		t.Fatal("tampered update was accepted")
	}
}
