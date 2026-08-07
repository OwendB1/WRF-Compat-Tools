package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	schemaVersion = 1
	maxBodyBytes  = 1 << 20
	maxEvents     = 256
	inviteTTL     = 6 * time.Hour
)

var version = "dev"

var errInvalidEnrollment = errors.New("invalid or expired enrollment code")
var errInvalidLabel = errors.New("invalid device label")

//go:embed update-public-key.pem
var updatePublicKeyPEM []byte

//go:embed steamos/install.sh
var steamosInstaller []byte

//go:embed steamos/uninstall.sh
var steamosUninstaller []byte

var (
	runIDPattern   = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
	labelPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)
	valuePattern   = regexp.MustCompile(`^[A-Za-z0-9._:+/-]{0,96}$`)
	versionPattern = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`)
	achPattern     = regexp.MustCompile(`(?i)\bACH\s*[=:]\s*([0-9]{1,4})\b`)
	acPattern      = regexp.MustCompile(`ACClient:\s*([A-Za-z]+)`)
	closePattern   = regexp.MustCompile(`Connection was closed with:\s*([0-9]{1,5})`)
	messagePattern = regexp.MustCompile(`Handler for message\s+([0-9]{1,10})`)
	logTimePattern = regexp.MustCompile(`^\[(\d{4}\.\d{2}\.\d{2}-\d{2}\.\d{2}\.\d{2}:\d{3})\]`)
)

type event struct {
	At         string `json:"at"`
	Type       string `json:"type"`
	State      string `json:"state,omitempty"`
	Name       string `json:"name,omitempty"`
	Version    string `json:"version,omitempty"`
	Code       int64  `json:"code,omitempty"`
	Length     int64  `json:"length,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Module     string `json:"module,omitempty"`
	RVA        string `json:"rva,omitempty"`
	Value      *bool  `json:"value,omitempty"`
}

type envelope struct {
	Schema       int     `json:"schema"`
	RunID        string  `json:"run_id"`
	DeviceLabel  string  `json:"device_label"`
	AgentVersion string  `json:"agent_version"`
	Mode         string  `json:"mode"`
	Events       []event `json:"events"`
}

type storedEvent struct {
	RunID        string `json:"run_id"`
	DeviceLabel  string `json:"device_label"`
	AgentVersion string `json:"agent_version"`
	Mode         string `json:"mode"`
	Event        event  `json:"event"`
}

type clientCredential struct {
	DeviceLabel string `json:"device_label"`
	TokenHash   string `json:"token_hash"`
	CreatedAt   string `json:"created_at"`
}

type enrollmentInvite struct {
	DeviceLabel string `json:"device_label"`
	CodeHash    string `json:"code_hash"`
	ExpiresAt   string `json:"expires_at"`
}

type credentialRegistry struct {
	Clients []clientCredential `json:"clients"`
	Invites []enrollmentInvite `json:"invites"`
}

type enrollmentRequest struct {
	Code string `json:"code"`
}

type enrollmentResponse struct {
	Token       string `json:"token"`
	DeviceLabel string `json:"device_label"`
}

var allowedEventTypes = map[string]bool{
	"session_start":   true,
	"session_end":     true,
	"runtime":         true,
	"game_build":      true,
	"ach":             true,
	"ac_state":        true,
	"mrac":            true,
	"backend_close":   true,
	"backend_message": true,
	"heartbeat":       true,
	"gate_result":     true,
	"pipe_state":      true,
	"thread":          true,
	"tls_callback":    true,
	"exception":       true,
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "serve":
		if err := runServer(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "agent":
		if err := runAgent(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "version":
		fmt.Println(version)
	case "healthcheck":
		if err := healthcheck(); err != nil {
			log.Fatal(err)
		}
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: wrf-collector <serve|agent|version|healthcheck>")
	os.Exit(2)
}

func healthcheck() error {
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://127.0.0.1:8080/healthz")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned %s", response.Status)
	}
	return nil
}

func validateEnvelope(e *envelope) error {
	if e.Schema != schemaVersion {
		return errors.New("unsupported schema")
	}
	if !runIDPattern.MatchString(e.RunID) {
		return errors.New("invalid run_id")
	}
	if !labelPattern.MatchString(e.DeviceLabel) {
		return errors.New("invalid device_label")
	}
	if !versionPattern.MatchString(e.AgentVersion) {
		return errors.New("invalid agent_version")
	}
	if e.Mode != "baseline" && e.Mode != "instrumented" {
		return errors.New("invalid mode")
	}
	if len(e.Events) == 0 || len(e.Events) > maxEvents {
		return errors.New("invalid event count")
	}
	for i := range e.Events {
		if err := validateEvent(&e.Events[i]); err != nil {
			return fmt.Errorf("event %d: %w", i, err)
		}
	}
	return nil
}

func validateEvent(e *event) error {
	if !allowedEventTypes[e.Type] {
		return errors.New("invalid type")
	}
	if _, err := time.Parse(time.RFC3339Nano, e.At); err != nil {
		return errors.New("invalid timestamp")
	}
	for _, value := range []string{e.State, e.Name, e.Version, e.Module, e.RVA} {
		if !valuePattern.MatchString(value) {
			return errors.New("invalid field")
		}
	}
	if e.Code < 0 || e.Code > 1<<32 || e.Length < 0 || e.Length > 1<<30 ||
		e.DurationMS < 0 || e.DurationMS > 24*60*60*1000 {
		return errors.New("numeric field out of range")
	}
	return nil
}

type server struct {
	dataDir       string
	adminToken    string
	publicURL     string
	agentBinary   string
	agentManifest string
	mu            sync.Mutex
}

func runServer(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := flags.String("listen", envOr("COLLECTOR_LISTEN", ":8080"), "listen address")
	dataDir := flags.String("data", envOr("COLLECTOR_DATA_DIR", "/data"), "data directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	admin := os.Getenv("COLLECTOR_ADMIN_TOKEN")
	if len(admin) < 32 {
		return errors.New("COLLECTOR_ADMIN_TOKEN must be at least 32 characters")
	}
	publicURL := strings.TrimRight(os.Getenv("COLLECTOR_PUBLIC_URL"), "/")
	parsedPublicURL, err := url.Parse(publicURL)
	if err != nil || parsedPublicURL.Scheme != "https" || parsedPublicURL.Host == "" || parsedPublicURL.User != nil || parsedPublicURL.Path != "" {
		return errors.New("COLLECTOR_PUBLIC_URL must be an HTTPS origin without a path or credentials")
	}
	if err := os.MkdirAll(*dataDir, 0700); err != nil {
		return err
	}
	s := &server{
		dataDir:       *dataDir,
		adminToken:    admin,
		publicURL:     publicURL,
		agentBinary:   envOr("COLLECTOR_AGENT_BINARY", "/opt/collector/wrf-collector"),
		agentManifest: envOr("COLLECTOR_AGENT_MANIFEST", "/opt/collector/agent-manifest.json"),
	}
	if err := s.cleanup(); err != nil {
		log.Printf("retention cleanup: %v", err)
	}
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := s.cleanup(); err != nil {
				log.Printf("retention cleanup: %v", err)
			}
		}
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/v1/events", s.ingest)
	mux.HandleFunc("/v1/enroll", s.enrollHandler)
	mux.HandleFunc("/v1/runs", s.runs)
	mux.HandleFunc("/v1/runs/", s.run)
	mux.HandleFunc("/v1/agent/manifest.json", s.agentManifestHandler)
	mux.HandleFunc("/v1/agent/linux-amd64", s.agentBinaryHandler)
	mux.HandleFunc("/v1/agent/update-public-key.pem", s.publicKeyHandler)
	mux.HandleFunc("/download/install.sh", s.installerHandler)
	mux.HandleFunc("/download/uninstall.sh", s.uninstallerHandler)
	mux.HandleFunc("/admin/invites", s.createInviteHandler)
	mux.HandleFunc("/admin", s.dashboard)
	mux.HandleFunc("/", s.landing)
	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("collector %s listening on %s", version, *listen)
	return httpServer.ListenAndServe()
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": version})
}

func (s *server) ingest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if media := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])); media != "application/json" {
		http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	var envelope envelope
	if err := decoder.Decode(&envelope); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "trailing JSON", http.StatusBadRequest)
		return
	}
	if err := validateEnvelope(&envelope); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if token == "" || !s.authenticateClient(token, envelope.DeviceLabel) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := s.appendEvents(&envelope); err != nil {
		log.Printf("store run %s: %v", envelope.RunID[:8], err)
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) enrollHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if media := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])); media != "application/json" {
		http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	var request enrollmentRequest
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "trailing JSON", http.StatusBadRequest)
		return
	}
	response, err := s.enroll(strings.TrimSpace(request.Code))
	if errors.Is(err, errInvalidEnrollment) {
		http.Error(w, errInvalidEnrollment.Error(), http.StatusUnauthorized)
		return
	}
	if err != nil {
		log.Printf("enrollment: %v", err)
		http.Error(w, "enrollment unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func hashSecret(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func randomHex(bytesCount int) (string, error) {
	value := make([]byte, bytesCount)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func (s *server) readRegistryLocked() (credentialRegistry, error) {
	data, err := os.ReadFile(filepath.Join(s.dataDir, "clients.json"))
	if errors.Is(err, os.ErrNotExist) {
		return credentialRegistry{}, nil
	}
	if err != nil {
		return credentialRegistry{}, err
	}
	var registry credentialRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return credentialRegistry{}, err
	}
	return registry, nil
}

func (s *server) writeRegistryLocked(registry *credentialRegistry) error {
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.dataDir, "clients.json")
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return os.Chmod(path, 0600)
}

func pruneInvites(registry *credentialRegistry, current time.Time) {
	kept := registry.Invites[:0]
	for _, invite := range registry.Invites {
		expires, err := time.Parse(time.RFC3339Nano, invite.ExpiresAt)
		if err == nil && expires.After(current) {
			kept = append(kept, invite)
		}
	}
	registry.Invites = kept
}

func (s *server) createInvite(label string) (string, time.Time, error) {
	label = strings.TrimSpace(label)
	if !labelPattern.MatchString(label) {
		return "", time.Time{}, errInvalidLabel
	}
	code, err := randomHex(16)
	if err != nil {
		return "", time.Time{}, err
	}
	expires := time.Now().UTC().Add(inviteTTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	registry, err := s.readRegistryLocked()
	if err != nil {
		return "", time.Time{}, err
	}
	pruneInvites(&registry, time.Now().UTC())
	kept := registry.Invites[:0]
	for _, invite := range registry.Invites {
		if invite.DeviceLabel != label {
			kept = append(kept, invite)
		}
	}
	registry.Invites = append(kept, enrollmentInvite{
		DeviceLabel: label,
		CodeHash:    hashSecret(code),
		ExpiresAt:   expires.Format(time.RFC3339Nano),
	})
	if err := s.writeRegistryLocked(&registry); err != nil {
		return "", time.Time{}, err
	}
	return code, expires, nil
}

func (s *server) enroll(code string) (enrollmentResponse, error) {
	if len(code) != 32 {
		return enrollmentResponse{}, errInvalidEnrollment
	}
	codeHash := hashSecret(strings.ToLower(code))
	s.mu.Lock()
	defer s.mu.Unlock()
	registry, err := s.readRegistryLocked()
	if err != nil {
		return enrollmentResponse{}, err
	}
	pruneInvites(&registry, time.Now().UTC())
	match := -1
	for index, invite := range registry.Invites {
		if constantEqual(invite.CodeHash, codeHash) {
			match = index
		}
	}
	if match < 0 {
		return enrollmentResponse{}, errInvalidEnrollment
	}
	label := registry.Invites[match].DeviceLabel
	token, err := randomHex(32)
	if err != nil {
		return enrollmentResponse{}, err
	}
	clients := registry.Clients[:0]
	for _, client := range registry.Clients {
		if client.DeviceLabel != label {
			clients = append(clients, client)
		}
	}
	registry.Clients = append(clients, clientCredential{
		DeviceLabel: label,
		TokenHash:   hashSecret(token),
		CreatedAt:   now(),
	})
	registry.Invites = append(registry.Invites[:match], registry.Invites[match+1:]...)
	if err := s.writeRegistryLocked(&registry); err != nil {
		return enrollmentResponse{}, err
	}
	return enrollmentResponse{Token: token, DeviceLabel: label}, nil
}

func (s *server) authenticateClient(token, label string) bool {
	if len(token) != 64 {
		return false
	}
	tokenHash := hashSecret(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	registry, err := s.readRegistryLocked()
	if err != nil {
		return false
	}
	for _, client := range registry.Clients {
		if client.DeviceLabel == label && constantEqual(client.TokenHash, tokenHash) {
			return true
		}
	}
	return false
}

func (s *server) appendEvents(envelope *envelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dataDir, envelope.RunID+".jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	for _, item := range envelope.Events {
		stored := storedEvent{
			RunID:        envelope.RunID,
			DeviceLabel:  envelope.DeviceLabel,
			AgentVersion: envelope.AgentVersion,
			Mode:         envelope.Mode,
			Event:        item,
		}
		if err := encoder.Encode(&stored); err != nil {
			return err
		}
	}
	return file.Sync()
}

func (s *server) authorizedAdmin(r *http.Request) bool {
	_, password, ok := r.BasicAuth()
	return ok && constantEqual(password, s.adminToken)
}

func (s *server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if s.authorizedAdmin(r) {
		return true
	}
	w.Header().Set("WWW-Authenticate", `Basic realm="WRF Collector", charset="UTF-8"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

func (s *server) runs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	runs, err := s.loadRuns()
	if err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *server) run(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	runID := strings.TrimPrefix(r.URL.Path, "/v1/runs/")
	if !runIDPattern.MatchString(runID) {
		http.NotFound(w, r)
		return
	}
	events, err := s.readRun(runID)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

type runSummary struct {
	RunID        string `json:"run_id"`
	DeviceLabel  string `json:"device_label"`
	Mode         string `json:"mode"`
	AgentVersion string `json:"agent_version"`
	Started      string `json:"started"`
	Last         string `json:"last"`
	ACOnline     bool   `json:"ac_online"`
	MRAC         bool   `json:"mrac"`
	GateLength   int64  `json:"gate_length"`
	CloseCode    int64  `json:"close_code"`
	EventCount   int    `json:"event_count"`
}

func (s *server) loadRuns() ([]runSummary, error) {
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return nil, err
	}
	runs := make([]runSummary, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		runID := strings.TrimSuffix(name, ".jsonl")
		if !runIDPattern.MatchString(runID) {
			continue
		}
		events, err := s.readRun(runID)
		if err != nil || len(events) == 0 {
			continue
		}
		summary := summarize(events)
		runs = append(runs, summary)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].Last > runs[j].Last })
	return runs, nil
}

func (s *server) readRun(runID string) ([]storedEvent, error) {
	file, err := os.Open(filepath.Join(s.dataDir, runID+".jsonl"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var events []storedEvent
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 256*1024)
	for scanner.Scan() {
		var item storedEvent
		if json.Unmarshal(scanner.Bytes(), &item) == nil {
			events = append(events, item)
		}
	}
	return events, scanner.Err()
}

func summarize(events []storedEvent) runSummary {
	first := events[0]
	summary := runSummary{
		RunID: first.RunID, DeviceLabel: first.DeviceLabel, Mode: first.Mode,
		AgentVersion: first.AgentVersion, Started: first.Event.At,
	}
	for _, item := range events {
		summary.Last = item.Event.At
		summary.EventCount++
		switch item.Event.Type {
		case "ac_state":
			if strings.EqualFold(item.Event.State, "online") {
				summary.ACOnline = true
			}
		case "mrac":
			summary.MRAC = true
		case "gate_result":
			summary.GateLength = item.Event.Length
		case "backend_close":
			summary.CloseCode = item.Event.Code
		}
	}
	return summary
}

var landingTemplate = template.Must(template.New("landing").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>WRF Steam Deck collector</title><style>
body{font:16px system-ui,sans-serif;max-width:760px;margin:3rem auto;padding:0 1.25rem;background:#111;color:#eee;line-height:1.5}a{color:#8cf}.button{display:inline-block;background:#2879d0;color:white;padding:.7rem 1rem;border-radius:.4rem;text-decoration:none;font-weight:600}code{background:#222;padding:.15rem .3rem;border-radius:.2rem}
</style></head><body><h1>WRF Steam Deck collector</h1>
<p>This volunteer tool records normalized compatibility timing and state events. It does not upload game logs, credentials, command lines, packet bodies, or memory dumps.</p>
<p><a class="button" href="/download/install.sh" download>Download SteamOS installer</a></p>
<ol><li>Download the installer.</li><li>Open Konsole in the download folder and run <code>chmod +x install.sh &amp;&amp; ./install.sh</code>.</li><li>Enter the one-time enrollment code supplied by the project administrator.</li></ol>
<p>The installer shows the collection scope before making changes and includes an easy uninstall command.</p>
<p><a href="/admin">Administrator dashboard</a></p></body></html>`))

func (s *server) landing(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := landingTemplate.Execute(w, nil); err != nil {
		log.Printf("landing page: %v", err)
	}
}

type dashboardData struct {
	Runs          []runSummary
	InviteCode    string
	InviteLabel   string
	InviteExpires string
}

var dashboardTemplate = template.Must(template.New("dashboard").Funcs(template.FuncMap{
	"short": func(value string) string {
		if len(value) > 8 {
			return value[:8]
		}
		return value
	},
	"yn": func(value bool) string {
		if value {
			return "yes"
		}
		return "no"
	},
}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>WRF collector</title><style>
body{font:14px system-ui,sans-serif;margin:2rem;background:#111;color:#eee}a{color:#8cf}table{border-collapse:collapse;width:100%}th,td{padding:.45rem;border-bottom:1px solid #444;text-align:left}th{color:#bbb}.yes{color:#7e7}.no{color:#e88}code{font-size:12px}input,button{font:inherit;padding:.4rem}.invite{padding:1rem;background:#232323;border-left:4px solid #7e7;margin:1rem 0}.invite code{font-size:16px}
</style></head><body><h1>WRF collector administration</h1><p><a href="/">Public download page</a></p>
<h2>Enroll a Steam Deck</h2><form method="post" action="/admin/invites"><label>Device label <input name="device_label" required pattern="[A-Za-z0-9._-]{1,32}" maxlength="32" placeholder="volunteer-deck"></label> <button type="submit">Create one-time code</button></form>
{{if .InviteCode}}<div class="invite"><strong>Code for {{.InviteLabel}}</strong><p><code>{{.InviteCode}}</code></p><p>Expires {{.InviteExpires}}. Send it privately; it works once. Re-enrolling this label replaces its previous token.</p></div>{{end}}
<h2>Runs</h2>
<table><thead><tr><th>Run</th><th>Device</th><th>Mode</th><th>Started</th><th>AC</th><th>MRAC</th><th>Gate</th><th>Close</th><th>Events</th></tr></thead><tbody>
{{range .Runs}}<tr><td><a href="/v1/runs/{{.RunID}}"><code>{{short .RunID}}</code></a></td><td>{{.DeviceLabel}}</td><td>{{.Mode}}</td><td>{{.Started}}</td><td class="{{yn .ACOnline}}">{{.ACOnline}}</td><td class="{{yn .MRAC}}">{{.MRAC}}</td><td>{{.GateLength}}</td><td>{{.CloseCode}}</td><td>{{.EventCount}}</td></tr>{{else}}<tr><td colspan="9">No runs received.</td></tr>{{end}}
</tbody></table></body></html>`))

func (s *server) dashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	runs, err := s.loadRuns()
	if err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := dashboardTemplate.Execute(w, dashboardData{Runs: runs}); err != nil {
		log.Printf("dashboard: %v", err)
	}
}

func (s *server) createInviteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	if !s.sameOrigin(r) {
		http.Error(w, "cross-origin request rejected", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	label := strings.TrimSpace(r.FormValue("device_label"))
	code, expires, err := s.createInvite(label)
	if errors.Is(err, errInvalidLabel) {
		http.Error(w, errInvalidLabel.Error(), http.StatusBadRequest)
		return
	}
	if err != nil {
		log.Printf("create invitation: %v", err)
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	runs, err := s.loadRuns()
	if err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := dashboardData{Runs: runs, InviteCode: code, InviteLabel: label, InviteExpires: expires.Format(time.RFC3339)}
	if err := dashboardTemplate.Execute(w, data); err != nil {
		log.Printf("dashboard: %v", err)
	}
}

func (s *server) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") {
		return false
	}
	publicURL, err := url.Parse(s.publicURL)
	if err == nil && equalOrigin(parsed, publicURL) {
		return true
	}
	forwardedHost := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0])
	return strings.EqualFold(parsed.Host, r.Host) ||
		(forwardedHost != "" && strings.EqualFold(parsed.Host, forwardedHost))
}

func equalOrigin(a, b *url.URL) bool {
	port := func(value *url.URL) string {
		if value.Port() != "" {
			return value.Port()
		}
		if strings.EqualFold(value.Scheme, "https") {
			return "443"
		}
		if strings.EqualFold(value.Scheme, "http") {
			return "80"
		}
		return ""
	}
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}

func (s *server) agentManifestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	http.ServeFile(w, r, s.agentManifest)
}

func (s *server) agentBinaryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="wrf-collector"`)
	http.ServeFile(w, r, s.agentBinary)
}

func (s *server) publicKeyHandler(w http.ResponseWriter, r *http.Request) {
	serveDownload(w, r, updatePublicKeyPEM, "update-public-key.pem", "application/x-pem-file")
}

func (s *server) installerHandler(w http.ResponseWriter, r *http.Request) {
	installer := bytes.ReplaceAll(steamosInstaller, []byte("https://collect.example.com"), []byte(s.publicURL))
	serveDownload(w, r, installer, "install.sh", "text/x-shellscript; charset=utf-8")
}

func (s *server) uninstallerHandler(w http.ResponseWriter, r *http.Request) {
	serveDownload(w, r, steamosUninstaller, "uninstall.sh", "text/x-shellscript; charset=utf-8")
}

func serveDownload(w http.ResponseWriter, r *http.Request, data []byte, name, contentType string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *server) cleanup() error {
	days, err := strconv.Atoi(envOr("COLLECTOR_RETENTION_DAYS", "30"))
	if err != nil || days < 1 || days > 3650 {
		return errors.New("COLLECTOR_RETENTION_DAYS must be between 1 and 3650")
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(s.dataDir, entry.Name()))
		}
	}
	return nil
}

func constantEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type agentConfig struct {
	URL         string `json:"url"`
	Token       string `json:"token"`
	DeviceLabel string `json:"device_label"`
	Mode        string `json:"mode"`
	LogPath     string `json:"log_path"`
	ProbePath   string `json:"probe_path"`
	AutoUpdate  bool   `json:"auto_update"`
}

type updateManifest struct {
	Version   string `json:"version"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
}

type agentState struct {
	config        agentConfig
	client        *http.Client
	runID         string
	pending       []event
	active        bool
	lastHeartbeat time.Time
	knownFile     os.FileInfo
	offset        int64
	partial       []byte
	probeOffset   int64
	probeKnown    bool
	probePartial  []byte
	nextUpdate    time.Time
}

func runAgent(args []string) error {
	flags := flag.NewFlagSet("agent", flag.ContinueOnError)
	configPath := flags.String("config", defaultConfigPath(), "agent config")
	if err := flags.Parse(args); err != nil {
		return err
	}
	config, err := loadAgentConfig(*configPath)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	if config.AutoUpdate {
		updated, err := updateAgent(client, config.URL)
		if err != nil {
			log.Printf("update check: %v", err)
		} else if updated {
			return nil
		}
	}
	state := &agentState{config: config, client: client, nextUpdate: time.Now().Add(6 * time.Hour)}
	log.Printf("agent %s watching normalized WRF events", version)
	return state.monitor()
}

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(dir, "wrf-collector", "config.json")
}

func loadAgentConfig(path string) (agentConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return agentConfig{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 64*1024))
	decoder.DisallowUnknownFields()
	var config agentConfig
	if err := decoder.Decode(&config); err != nil {
		return config, err
	}
	parsed, err := url.Parse(config.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return config, errors.New("collector URL must be HTTPS without embedded credentials")
	}
	config.URL = strings.TrimRight(config.URL, "/")
	if len(config.Token) < 32 || !labelPattern.MatchString(config.DeviceLabel) {
		return config, errors.New("invalid token or device label")
	}
	if config.Mode != "baseline" && config.Mode != "instrumented" {
		return config, errors.New("mode must be baseline or instrumented")
	}
	if config.LogPath == "" {
		return config, errors.New("log_path is required")
	}
	return config, nil
}

func (a *agentState) monitor() error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if a.config.AutoUpdate && time.Now().After(a.nextUpdate) {
			if _, err := updateAgent(a.client, a.config.URL); err != nil {
				log.Printf("update check: %v", err)
			}
			a.nextUpdate = time.Now().Add(6 * time.Hour)
		}
		if err := a.pollGameLog(); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("game log: %v", err)
		}
		if err := a.pollProbe(); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("probe metadata: %v", err)
		}
		if a.active && time.Since(a.lastHeartbeat) >= 30*time.Second {
			a.queue(event{At: now(), Type: "heartbeat", State: "active"})
			a.lastHeartbeat = time.Now()
		}
		if len(a.pending) > 0 {
			if err := a.flush(); err != nil {
				log.Printf("upload deferred: %v", err)
			}
		}
		<-ticker.C
	}
}

func (a *agentState) pollGameLog() error {
	info, err := os.Stat(a.config.LogPath)
	if err != nil {
		return err
	}
	if a.knownFile == nil {
		a.knownFile = info
		if time.Since(info.ModTime()) < 30*time.Second {
			a.startRun()
			a.offset = 0
		} else {
			a.offset = info.Size()
		}
	}
	if !os.SameFile(a.knownFile, info) || info.Size() < a.offset {
		a.knownFile = info
		a.offset = 0
		a.partial = nil
		a.startRun()
	}
	if info.Size() == a.offset {
		return nil
	}
	file, err := os.Open(a.config.LogPath)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Seek(a.offset, io.SeekStart); err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(file, 4*1024*1024))
	if err != nil {
		return err
	}
	a.offset += int64(len(data))
	data = append(a.partial, data...)
	lines := bytes.Split(data, []byte{'\n'})
	a.partial = append([]byte(nil), lines[len(lines)-1]...)
	for _, raw := range lines[:len(lines)-1] {
		if item, ok := parseGameLine(string(bytes.TrimSpace(raw))); ok {
			if a.runID == "" {
				a.startRun()
			}
			a.queue(item)
			if item.Type == "backend_close" {
				a.queue(event{At: now(), Type: "session_end", State: "backend_close", Code: item.Code})
				a.active = false
			}
		}
	}
	return nil
}

func (a *agentState) pollProbe() error {
	if a.config.ProbePath == "" {
		return nil
	}
	file, err := os.Open(a.config.ProbePath)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !a.probeKnown {
		a.probeKnown = true
		a.probeOffset = info.Size()
		return nil
	}
	if info.Size() < a.probeOffset {
		a.probeOffset = 0
		a.probePartial = nil
	}
	if _, err := file.Seek(a.probeOffset, io.SeekStart); err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil {
		return err
	}
	a.probeOffset += int64(len(data))
	data = append(a.probePartial, data...)
	lines := bytes.Split(data, []byte{'\n'})
	a.probePartial = append([]byte(nil), lines[len(lines)-1]...)
	for _, line := range lines[:len(lines)-1] {
		var item event
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&item) == nil && validateEvent(&item) == nil {
			if a.runID == "" {
				a.startRun()
			}
			a.queue(item)
		}
	}
	return nil
}

func (a *agentState) startRun() {
	if a.runID != "" {
		if a.active {
			a.queue(event{At: now(), Type: "session_end", State: "new_log"})
		}
		if err := a.flush(); err != nil {
			log.Printf("previous run final upload: %v", err)
			a.pending = nil
		}
	}
	a.runID = newRunID()
	a.active = true
	a.lastHeartbeat = time.Now()
	a.queue(event{At: now(), Type: "session_start", State: "started"})
	if name, value := osRelease(); name != "" {
		a.queue(event{At: now(), Type: "runtime", Name: name, Version: value})
	}
	if kernel := safeVersion(readSmallFile("/proc/sys/kernel/osrelease")); kernel != "" {
		a.queue(event{At: now(), Type: "runtime", Name: "kernel", Version: kernel})
	}
	if proton := steamProtonVersion(); proton != "" {
		a.queue(event{At: now(), Type: "runtime", Name: "proton", Version: proton})
	}
	if build := steamBuildID(); build != "" {
		a.queue(event{At: now(), Type: "game_build", Version: build})
	}
}

func (a *agentState) queue(item event) {
	if item.At == "" {
		item.At = now()
	}
	if validateEvent(&item) != nil {
		return
	}
	a.pending = append(a.pending, item)
	if len(a.pending) > 1024 {
		a.pending = append([]event(nil), a.pending[len(a.pending)-1024:]...)
	}
}

func (a *agentState) flush() error {
	if a.runID == "" {
		return nil
	}
	count := len(a.pending)
	if count > maxEvents {
		count = maxEvents
	}
	envelope := envelope{
		Schema: schemaVersion, RunID: a.runID, DeviceLabel: a.config.DeviceLabel,
		AgentVersion: safeVersion(version), Mode: a.config.Mode,
		Events: append([]event(nil), a.pending[:count]...),
	}
	body, err := json.Marshal(&envelope)
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, a.config.URL+"/v1/events", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+a.config.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("server returned %s", response.Status)
	}
	a.pending = a.pending[count:]
	return nil
}

func parseGameLine(line string) (event, bool) {
	stamp := eventTime(line)
	if match := acPattern.FindStringSubmatch(line); match != nil {
		return event{At: stamp, Type: "ac_state", State: strings.ToLower(match[1])}, true
	}
	if match := closePattern.FindStringSubmatch(line); match != nil {
		code, _ := strconv.ParseInt(match[1], 10, 64)
		return event{At: stamp, Type: "backend_close", Code: code}, true
	}
	if strings.Contains(line, "FMracServiceWs::ClientRequest") {
		state := "observed"
		lower := strings.ToLower(line)
		if strings.Contains(lower, "fail") || strings.Contains(lower, "error") {
			state = "failure"
		} else if strings.Contains(lower, "response") || strings.Contains(lower, "received") {
			state = "response"
		} else if strings.Contains(lower, "call") || strings.Contains(lower, "request") {
			state = "call"
		}
		return event{At: stamp, Type: "mrac", State: state}, true
	}
	if match := achPattern.FindStringSubmatch(line); match != nil {
		code, _ := strconv.ParseInt(match[1], 10, 64)
		return event{At: stamp, Type: "ach", Code: code}, true
	}
	if match := messagePattern.FindStringSubmatch(line); match != nil {
		code, _ := strconv.ParseInt(match[1], 10, 64)
		return event{At: stamp, Type: "backend_message", Code: code}, true
	}
	return event{}, false
}

func eventTime(line string) string {
	match := logTimePattern.FindStringSubmatch(line)
	if match == nil {
		return now()
	}
	value := match[1]
	value = value[:len(value)-4] + "." + value[len(value)-3:]
	parsed, err := time.ParseInLocation("2006.01.02-15.04.05.000", value, time.UTC)
	if err != nil {
		return now()
	}
	return parsed.Format(time.RFC3339Nano)
}

func newRunID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	hexValue := hex.EncodeToString(value)
	return hexValue[:8] + "-" + hexValue[8:12] + "-" + hexValue[12:16] + "-" + hexValue[16:20] + "-" + hexValue[20:]
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func osRelease() (string, string) {
	data := readSmallFile("/etc/os-release")
	values := map[string]string{}
	for _, line := range strings.Split(data, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = strings.Trim(strings.TrimSpace(value), `"`)
		}
	}
	return safeVersion(values["ID"]), safeVersion(values["VERSION_ID"])
}

func steamBuildID() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data := readSmallFile(filepath.Join(dir, ".local/share/Steam/steamapps/appmanifest_1491000.acf"))
	pattern := regexp.MustCompile(`"buildid"\s+"([0-9]{1,20})"`)
	match := pattern.FindStringSubmatch(data)
	if match == nil {
		return ""
	}
	return match[1]
}

func steamProtonVersion() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data := readSmallFile(filepath.Join(dir, ".local/share/Steam/steamapps/compatdata/1491000/config_info"))
	first, _, _ := strings.Cut(data, "\n")
	if first == "" {
		return ""
	}
	return safeVersion(first)
}

func readSmallFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 1<<20 {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func safeVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if versionPattern.MatchString(value) {
		return value
	}
	return "unknown"
}

func updateAgent(client *http.Client, baseURL string) (bool, error) {
	if version == "dev" {
		return false, nil
	}
	manifestURL := strings.TrimRight(baseURL, "/") + "/v1/agent/manifest.json"
	response, err := client.Get(manifestURL)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("manifest returned %s", response.Status)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 32*1024))
	decoder.DisallowUnknownFields()
	var manifest updateManifest
	if err := decoder.Decode(&manifest); err != nil {
		return false, err
	}
	if manifest.Version == version {
		return false, nil
	}
	if !versionPattern.MatchString(manifest.Version) || len(manifest.SHA256) != 64 {
		return false, errors.New("invalid update manifest")
	}
	binaryURL := strings.TrimRight(baseURL, "/") + "/v1/agent/linux-amd64"
	binaryResponse, err := client.Get(binaryURL)
	if err != nil {
		return false, err
	}
	defer binaryResponse.Body.Close()
	if binaryResponse.StatusCode != http.StatusOK {
		return false, fmt.Errorf("binary returned %s", binaryResponse.Status)
	}
	binary, err := io.ReadAll(io.LimitReader(binaryResponse.Body, 64*1024*1024+1))
	if err != nil || len(binary) > 64*1024*1024 {
		return false, errors.New("invalid update binary")
	}
	if err := verifyUpdate(binary, &manifest); err != nil {
		return false, err
	}
	executable, err := os.Executable()
	if err != nil {
		return false, err
	}
	temporary := executable + ".new"
	if err := os.WriteFile(temporary, binary, 0755); err != nil {
		return false, err
	}
	if err := os.Rename(temporary, executable); err != nil {
		_ = os.Remove(temporary)
		return false, err
	}
	log.Printf("updated agent %s -> %s", version, manifest.Version)
	return true, syscall.Exec(executable, os.Args, os.Environ())
}

func verifyUpdate(binary []byte, manifest *updateManifest) error {
	digest := sha256.Sum256(binary)
	if !constantEqual(hex.EncodeToString(digest[:]), strings.ToLower(manifest.SHA256)) {
		return errors.New("update checksum mismatch")
	}
	signature, err := base64.StdEncoding.DecodeString(manifest.Signature)
	if err != nil {
		return errors.New("invalid update signature")
	}
	block, _ := pem.Decode(updatePublicKeyPEM)
	if block == nil {
		return errors.New("invalid embedded public key")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return err
	}
	publicKey, ok := parsed.(ed25519.PublicKey)
	if !ok || !ed25519.Verify(publicKey, binary, signature) {
		return errors.New("update signature verification failed")
	}
	return nil
}
