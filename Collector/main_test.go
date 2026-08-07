package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

	response = httptest.NewRecorder()
	server.dashboard(response, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("admin status = %d", response.Code)
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
