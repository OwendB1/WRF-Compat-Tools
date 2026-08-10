package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/hmac"
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
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	schemaVersion            = 1
	maxBodyBytes             = 1 << 20
	maxEvents                = 2048
	maxPendingEvents         = 65536
	maxProtonReadBytes       = 384 << 10
	maxDiagnosticBinaryBytes = 128 << 20
	maxMRACPayloadBytes      = 128 << 10
	inviteTTL                = 6 * time.Hour
	filetimeUnixOffset       = 116444736000000000
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
	runIDPattern        = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
	labelPattern        = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)
	valuePattern        = regexp.MustCompile(`^[A-Za-z0-9._:+/-]{0,96}$`)
	versionPattern      = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`)
	sha256Pattern       = regexp.MustCompile(`^[a-f0-9]{64}$`)
	hexValuePattern     = regexp.MustCompile(`^(0x)?[A-Fa-f0-9]{1,16}$`)
	platformTextPattern = regexp.MustCompile(`^[A-Za-z0-9 ._:+/()#-]{1,128}$`)
	achPattern          = regexp.MustCompile(`(?i)\bACH\s*[=:]\s*([0-9]{1,4})\b`)
	acPattern           = regexp.MustCompile(`(?i)\bACClient:\s*(Online|Offline)\b`)
	closePattern        = regexp.MustCompile(`Connection was closed with:\s*([0-9]{1,5})`)
	messagePattern      = regexp.MustCompile(`Handler for message\s+([0-9]{1,10})`)
	rpcPattern          = regexp.MustCompile(`(?i)\[RPC\s+(Call|Response|Error|Failure)\s+\(([0-9]{1,10})\)[^]]*\]\s+([A-Za-z0-9_:]{1,96})`)
	sendPattern         = regexp.MustCompile(`(?i)Trying to send\s+([0-9]{1,10})\s+of data`)
	receivePattern      = regexp.MustCompile(`(?i)Received message:\s+with id\s+([0-9]{1,10})\s+with data`)
	responsePattern     = regexp.MustCompile(`(?i)Received server response\s+\(([0-9]{1,10})\s+bytes\)`)
	verbosityPattern    = regexp.MustCompile(`(?i)Log category\s+([A-Za-z0-9_]{1,64})\s+verbosity has been raised to\s+([A-Za-z]+)`)
	logTimePattern      = regexp.MustCompile(`^\[(\d{4}\.\d{2}\.\d{2}-\d{2}\.\d{2}\.\d{2}:\d{3})\]`)
)

var observedEnvironmentKeys = []string{"GC_PIPE_NAME", "GC_PROJECT_ID", "SteamDeck", "SteamAppId", "SteamGameId", "SteamEnv"}

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
	ThreadID   string `json:"thread_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Base       string `json:"base,omitempty"`
	Target     string `json:"target,omitempty"`
	Flags      int64  `json:"flags,omitempty"`
	Parameters int64  `json:"parameters,omitempty"`
	Access     *int64 `json:"access,omitempty"`
	Status     int64  `json:"status,omitempty"`
	Result     *bool  `json:"result,omitempty"`
	Size       int64  `json:"size,omitempty"`
	Value      *bool  `json:"value,omitempty"`
	Threads    int64  `json:"threads,omitempty"`
	FDs        int64  `json:"fds,omitempty"`
	Sockets    int64  `json:"sockets,omitempty"`
	PayloadB64 string `json:"payload_base64,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	Text       string `json:"text,omitempty"`
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
	"session_start":       true,
	"session_end":         true,
	"runtime":             true,
	"game_build":          true,
	"ach":                 true,
	"ac_state":            true,
	"mrac":                true,
	"backend_close":       true,
	"backend_message":     true,
	"heartbeat":           true, // Legacy baseline agent event.
	"collector_heartbeat": true,
	"collector_state":     true,
	"gate_result":         true,
	"pipe_state":          true,
	"thread":              true,
	"tls_callback":        true,
	"dllmain":             true,
	"module":              true,
	"exception":           true,
	"lifecycle":           true,
	"process_state":       true,
	"diagnostic":          true,
	"backend_state":       true,
	"rpc":                 true,
	"transport":           true,
	"env_flag":            true,
	"process_sample":      true,
	"process_runtime":     true,
	"platform_profile":    true,
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
	if e.Mode != "baseline" && e.Mode != "instrumented" && e.Mode != "steamdeck_reference" {
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
	for _, value := range []string{e.State, e.Name, e.Version, e.Module, e.RVA, e.ThreadID, e.Reason, e.Base, e.Target} {
		if !valuePattern.MatchString(value) {
			return errors.New("invalid field")
		}
	}
	for _, value := range []string{e.RVA, e.ThreadID, e.Base, e.Target} {
		if value != "" && !hexValuePattern.MatchString(value) {
			return errors.New("invalid hexadecimal field")
		}
	}
	if e.Code < 0 || e.Code > 1<<32 || e.Length < 0 || e.Length > 1<<30 ||
		e.Threads < 0 || e.Threads > 1<<20 || e.FDs < 0 || e.FDs > 1<<20 ||
		e.Sockets < 0 || e.Sockets > 1<<20 || e.Flags < 0 || e.Flags > 1<<32 ||
		e.Parameters < 0 || e.Parameters > 16 || e.Status < 0 || e.Status > 1<<32 ||
		e.Size < 0 || e.Size > 1<<32 ||
		e.DurationMS < 0 || e.DurationMS > 24*60*60*1000 {
		return errors.New("numeric field out of range")
	}
	if e.Access != nil && (e.Type != "exception" || (*e.Access != 0 && *e.Access != 1 && *e.Access != 8)) {
		return errors.New("invalid exception access type")
	}
	if e.Target != "" && e.Type != "exception" {
		return errors.New("fault target is only valid for exceptions")
	}
	if e.Text != "" && (e.Type != "platform_profile" || e.State != "dmi" || !platformTextPattern.MatchString(e.Text)) {
		return errors.New("text is only valid for DMI platform profile events")
	}
	if e.PayloadB64 != "" || e.SHA256 != "" {
		if e.Type != "mrac" || !strings.EqualFold(e.Name, "FMracServiceWs::ClientRequest") ||
			(e.State != "call" && e.State != "response") {
			return errors.New("payload is only valid for MRAC ClientRequest calls and responses")
		}
		payload, err := base64.StdEncoding.Strict().DecodeString(e.PayloadB64)
		if err != nil || len(payload) == 0 || len(payload) > maxMRACPayloadBytes {
			return errors.New("invalid MRAC payload")
		}
		digest := sha256.Sum256(payload)
		if !sha256Pattern.MatchString(e.SHA256) || e.SHA256 != hex.EncodeToString(digest[:]) {
			return errors.New("invalid MRAC payload digest")
		}
		if e.Length != int64(len(payload)) {
			return errors.New("invalid MRAC payload length")
		}
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
	mux.HandleFunc("/admin/runs/delete", s.deleteRunHandler)
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
	RunID         string `json:"run_id"`
	DeviceLabel   string `json:"device_label"`
	Mode          string `json:"mode"`
	AgentVersion  string `json:"agent_version"`
	Started       string `json:"started"`
	Last          string `json:"last"`
	Ended         string `json:"ended,omitempty"`
	Duration      string `json:"duration"`
	ACOnline      bool   `json:"ac_online"`
	MRAC          bool   `json:"mrac"`
	MRACState     string `json:"mrac_state"`
	MRACAttempts  int    `json:"mrac_attempts"`
	MRACCalls     int    `json:"mrac_calls"`
	MRACResponses int    `json:"mrac_responses"`
	MRACFailures  int    `json:"mrac_failures"`
	MRACPayloads  int    `json:"mrac_payloads"`
	RPCCalls      int    `json:"rpc_calls"`
	RPCResponses  int    `json:"rpc_responses"`
	PingCalls     int    `json:"ping_calls"`
	PingResponses int    `json:"ping_responses"`
	PingToClose   string `json:"ping_to_close,omitempty"`
	Backend       string `json:"backend"`
	CollectorGaps int    `json:"collector_gaps"`
	LongestGap    string `json:"longest_gap,omitempty"`
	Diagnostics   bool   `json:"diagnostics"`
	GateObserved  bool   `json:"gate_observed"`
	GateLength    int64  `json:"gate_length"`
	CloseCode     int64  `json:"close_code"`
	ProbeEvents   int    `json:"probe_events"`
	ProbeFaults   int    `json:"probe_faults"`
	ProbeTargets  int    `json:"probe_targets"`
	ProbeSlices   int    `json:"probe_slices"`
	EventCount    int    `json:"event_count"`
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
	lastPingResponse := ""
	probeTargets := make(map[string]bool)
	var lastProbeFault time.Time
	summary := runSummary{
		RunID: first.RunID, DeviceLabel: first.DeviceLabel, Mode: first.Mode,
		AgentVersion: first.AgentVersion, Started: first.Event.At,
	}
	for _, item := range events {
		summary.Last = item.Event.At
		summary.EventCount++
		switch item.Event.Type {
		case "session_end":
			summary.Ended = item.Event.At
		case "ac_state":
			if strings.EqualFold(item.Event.State, "online") {
				summary.ACOnline = true
			}
		case "mrac":
			summary.MRAC = true
			summary.MRACState = item.Event.State
			switch item.Event.State {
			case "client_request":
				summary.MRACAttempts++
			case "call":
				summary.MRACCalls++
			case "response":
				summary.MRACResponses++
			case "failure":
				summary.MRACFailures++
			}
			if item.Event.PayloadB64 != "" {
				summary.MRACPayloads++
			}
		case "rpc":
			if item.Event.State == "call" {
				summary.RPCCalls++
			} else if item.Event.State == "response" {
				summary.RPCResponses++
			}
			if strings.EqualFold(item.Event.Name, "FSessionProviderServiceWs::Ping") {
				if item.Event.State == "call" {
					summary.PingCalls++
				} else if item.Event.State == "response" {
					summary.PingResponses++
					lastPingResponse = item.Event.At
				}
			}
		case "backend_state":
			summary.Backend = item.Event.State
		case "diagnostic":
			if strings.EqualFold(item.Event.Name, "GLogBackendRpcWsCalls") {
				summary.Diagnostics = true
			}
		case "gate_result":
			summary.GateObserved = true
			summary.GateLength = item.Event.Length
		case "backend_close":
			summary.CloseCode = item.Event.Code
			summary.Backend = "closed"
			if pingAt, err := time.Parse(time.RFC3339Nano, lastPingResponse); err == nil {
				if closeAt, err := time.Parse(time.RFC3339Nano, item.Event.At); err == nil && !closeAt.Before(pingAt) {
					summary.PingToClose = closeAt.Sub(pingAt).Round(time.Millisecond).String()
				}
			}
		case "collector_state":
			if item.Event.State == "resumed" {
				summary.CollectorGaps++
				gap := time.Duration(item.Event.DurationMS) * time.Millisecond
				longest, _ := time.ParseDuration(summary.LongestGap)
				if gap > longest {
					summary.LongestGap = gap.String()
				}
			}
		case "module", "thread", "tls_callback", "dllmain", "exception":
			summary.ProbeEvents++
			if item.Event.Type == "exception" {
				summary.ProbeFaults++
				if item.Event.Target != "" {
					probeTargets[item.Event.Target] = true
				}
				if at, err := time.Parse(time.RFC3339Nano, item.Event.At); err == nil {
					if lastProbeFault.IsZero() || at.Sub(lastProbeFault) > 200*time.Millisecond {
						summary.ProbeSlices++
					}
					lastProbeFault = at
				}
			}
		}
	}
	summary.ProbeTargets = len(probeTargets)
	end := summary.Ended
	if end == "" {
		end = summary.Last
	}
	startedAt, startErr := time.Parse(time.RFC3339Nano, summary.Started)
	endedAt, endErr := time.Parse(time.RFC3339Nano, end)
	if startErr == nil && endErr == nil && !endedAt.Before(startedAt) {
		summary.Duration = endedAt.Sub(startedAt).Round(time.Millisecond).String()
	}
	return summary
}

var landingTemplate = template.Must(template.New("landing").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>WRF Steam Deck collector</title><style>
body{font:16px system-ui,sans-serif;max-width:760px;margin:3rem auto;padding:0 1.25rem;background:#111;color:#eee;line-height:1.5}a{color:#8cf}.button{display:inline-block;background:#2879d0;color:white;padding:.7rem 1rem;border-radius:.4rem;text-decoration:none;font-weight:600}code{background:#222;padding:.15rem .3rem;border-radius:.2rem}.notice{padding:1rem;background:#2b2415;border-left:4px solid #fc6}
</style></head><body><h1>WRF Steam Deck collector</h1>
<p>This volunteer tool permanently enables the complete diagnostic profile: verbose WRF anti-cheat/backend logging; normalized timing, lifecycle, RPC, transport, collector-liveness, runtime, process, environment-presence, hash, size, state, and approved Proton-probe events including exception access types and fault target addresses; and complete Base64 MRAC ClientRequest request and response blobs.</p>
<div class="notice"><strong>Data notice:</strong> MRAC blobs may contain opaque device or session attestation data. They are uploaded for administrator-only comparison and retained for the server's configured study period. Source logs, credentials, tokens, environment values, command lines, memory dumps, packet captures, and every unrelated RPC body stay local.</div>
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
	CSRFToken     string
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
body{font:14px system-ui,sans-serif;margin:2rem;background:#111;color:#eee}a{color:#8cf}table{border-collapse:collapse;width:100%}th,td{padding:.45rem;border-bottom:1px solid #444;text-align:left}th{color:#bbb}.yes{color:#7e7}.no{color:#e88}code{font-size:12px}input,button{font:inherit;padding:.4rem}.invite{padding:1rem;background:#232323;border-left:4px solid #7e7;margin:1rem 0}.invite code{font-size:16px}.delete{color:#fbb}
</style></head><body><h1>WRF collector administration</h1><p><a href="/">Public download page</a></p>
<h2>Enroll a Steam Deck</h2><form method="post" action="/admin/invites"><input type="hidden" name="csrf_token" value="{{.CSRFToken}}"><label>Device label <input name="device_label" required pattern="[A-Za-z0-9._-]{1,32}" maxlength="32" placeholder="volunteer-deck"></label> <button type="submit">Create one-time code</button></form>
{{if .InviteCode}}<div class="invite"><strong>Code for {{.InviteLabel}}</strong><p><code>{{.InviteCode}}</code></p><p>Expires {{.InviteExpires}}. Send it privately; it works once. Re-enrolling this label replaces its previous token.</p></div>{{end}}
<h2>Runs</h2>
<p>Delete removes a run permanently. An active run may reappear until its client exits.</p>
<table><thead><tr><th>Run</th><th>Device</th><th>Mode</th><th>Agent version</th><th>From</th><th>Until</th><th>Duration</th><th>Logs</th><th>AC</th><th>MRAC A/C/R/F</th><th>Payloads</th><th>RPC C/R</th><th>Ping C/R</th><th>Ping→Close</th><th>Probe E/X/T/S</th><th>Agent gaps</th><th>Backend</th><th>Gate</th><th>Close</th><th>Events</th><th>Action</th></tr></thead><tbody>
{{range .Runs}}<tr><td><a href="/v1/runs/{{.RunID}}"><code>{{short .RunID}}</code></a></td><td>{{.DeviceLabel}}</td><td>{{.Mode}}</td><td><code>{{.AgentVersion}}</code></td><td>{{.Started}}</td><td>{{if .Ended}}{{.Ended}}{{else}}{{.Last}} (active){{end}}</td><td>{{.Duration}}</td><td class="{{yn .Diagnostics}}">{{.Diagnostics}}</td><td class="{{yn .ACOnline}}">{{.ACOnline}}</td><td>{{.MRACAttempts}}/{{.MRACCalls}}/{{.MRACResponses}}/{{.MRACFailures}}</td><td>{{.MRACPayloads}}</td><td>{{.RPCCalls}}/{{.RPCResponses}}</td><td>{{.PingCalls}}/{{.PingResponses}}</td><td>{{.PingToClose}}</td><td>{{.ProbeEvents}}/{{.ProbeFaults}}/{{.ProbeTargets}}/{{.ProbeSlices}}</td><td>{{.CollectorGaps}}{{if .LongestGap}} / {{.LongestGap}}{{end}}</td><td>{{.Backend}}</td><td>{{if .GateObserved}}{{.GateLength}}{{else}}—{{end}}</td><td>{{.CloseCode}}</td><td>{{.EventCount}}</td><td><a class="delete" href="/admin/runs/delete?run_id={{.RunID}}">Delete</a></td></tr>{{else}}<tr><td colspan="21">No runs received.</td></tr>{{end}}
</tbody></table></body></html>`))

var deleteRunTemplate = template.Must(template.New("delete-run").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Delete WRF run</title><style>body{font:16px system-ui,sans-serif;max-width:760px;margin:3rem auto;padding:0 1.25rem;background:#111;color:#eee}a{color:#8cf}code{background:#222;padding:.15rem .3rem}button{font:inherit;padding:.6rem;color:#fbb;background:#321;border:1px solid #844}</style></head>
<body><h1>Delete run?</h1><p>This permanently removes <code>{{.RunID}}</code>. It cannot be recovered.</p><form method="post" action="/admin/runs/delete"><input type="hidden" name="csrf_token" value="{{.CSRFToken}}"><input type="hidden" name="run_id" value="{{.RunID}}"><button type="submit">Permanently delete</button></form><p><a href="/admin">Cancel</a></p></body></html>`))

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
	if err := dashboardTemplate.Execute(w, dashboardData{Runs: runs, CSRFToken: s.csrfToken()}); err != nil {
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
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if !constantEqual(r.FormValue("csrf_token"), s.csrfToken()) {
		http.Error(w, "invalid form token", http.StatusForbidden)
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
	data := dashboardData{Runs: runs, CSRFToken: s.csrfToken(), InviteCode: code, InviteLabel: label, InviteExpires: expires.Format(time.RFC3339)}
	if err := dashboardTemplate.Execute(w, data); err != nil {
		log.Printf("dashboard: %v", err)
	}
}

func (s *server) deleteRunHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		runID := r.URL.Query().Get("run_id")
		if !runIDPattern.MatchString(runID) {
			http.Error(w, "invalid run", http.StatusBadRequest)
			return
		}
		if _, err := os.Stat(filepath.Join(s.dataDir, runID+".jsonl")); errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		} else if err != nil {
			http.Error(w, "storage error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := deleteRunTemplate.Execute(w, struct{ RunID, CSRFToken string }{runID, s.csrfToken()}); err != nil {
			log.Printf("delete confirmation: %v", err)
		}
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if !constantEqual(r.FormValue("csrf_token"), s.csrfToken()) {
		http.Error(w, "invalid form token", http.StatusForbidden)
		return
	}
	runID := r.FormValue("run_id")
	if !runIDPattern.MatchString(runID) {
		http.Error(w, "invalid run", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	err := os.Remove(filepath.Join(s.dataDir, runID+".jsonl"))
	s.mu.Unlock()
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("delete run %s: %v", runID, err)
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	log.Printf("deleted run %s", runID)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *server) csrfToken() string {
	mac := hmac.New(sha256.New, []byte(s.adminToken))
	_, _ = mac.Write([]byte("admin-form"))
	return hex.EncodeToString(mac.Sum(nil))
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
	URL           string `json:"url"`
	Token         string `json:"token"`
	DeviceLabel   string `json:"device_label"`
	Mode          string `json:"mode"`
	LogPath       string `json:"log_path"`
	ProbePath     string `json:"probe_path"`
	ProtonLogPath string `json:"proton_log_path,omitempty"`
	AutoUpdate    bool   `json:"auto_update"`
}

type updateManifest struct {
	Version   string `json:"version"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
}

type agentState struct {
	config             agentConfig
	client             *http.Client
	runID              string
	pending            []event
	active             bool
	lastLiveness       time.Time
	lastLoop           time.Time
	knownFile          os.FileInfo
	offset             int64
	partial            []byte
	probeOffset        int64
	probeKnown         bool
	probePartial       []byte
	protonFile         os.FileInfo
	protonOffset       int64
	protonPartial      []byte
	nextUpdate         time.Time
	processKnown       bool
	gameRunning        bool
	lastProcessSample  time.Time
	processRuntimeSent bool
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
			return errors.New("agent updated; restarting")
		}
	}
	state := &agentState{config: config, client: client, nextUpdate: time.Now().Add(6 * time.Hour)}
	log.Printf("agent %s watching the complete WRF diagnostic profile", version)
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
	if config.Mode == "instrumented" {
		config.Mode = "steamdeck_reference"
	}
	if config.Mode != "baseline" && config.Mode != "steamdeck_reference" {
		return config, errors.New("mode must be baseline or steamdeck_reference")
	}
	if config.LogPath == "" {
		return config, errors.New("log_path is required")
	}
	if config.ProtonLogPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			config.ProtonLogPath = filepath.Join(home, "steam-1491000.log")
		}
	}
	return config, nil
}

func (a *agentState) monitor() error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		loopAt := time.Now()
		if item, ok := collectorGap(a.lastLoop, loopAt); ok && a.active {
			a.queue(item)
		}
		a.lastLoop = loopAt
		if a.config.AutoUpdate && time.Now().After(a.nextUpdate) {
			updated, err := updateAgent(a.client, a.config.URL)
			if err != nil {
				log.Printf("update check: %v", err)
			} else if updated {
				return errors.New("agent updated; restarting")
			}
			a.nextUpdate = time.Now().Add(6 * time.Hour)
		}
		if err := a.pollGameLog(); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("game log: %v", err)
		}
		a.pollGameProcess()
		if err := a.pollProbe(); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("probe metadata: %v", err)
		}
		if err := a.pollProtonLog(); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("Proton log: %v", err)
		}
		if a.active && time.Since(a.lastLiveness) >= 30*time.Second {
			a.queue(event{At: now(), Type: "collector_heartbeat", State: "active"})
			a.lastLiveness = time.Now()
		}
		if len(a.pending) > 0 {
			if err := a.flush(); err != nil {
				log.Printf("upload deferred: %v", err)
			}
		}
		<-ticker.C
	}
}

func collectorGap(previous, current time.Time) (event, bool) {
	if previous.IsZero() || current.Sub(previous) < 5*time.Second {
		return event{}, false
	}
	duration := current.Sub(previous)
	if duration > 24*time.Hour {
		duration = 24 * time.Hour
	}
	return event{At: current.UTC().Format(time.RFC3339Nano), Type: "collector_state", State: "resumed", DurationMS: duration.Milliseconds()}, true
}

func (a *agentState) pollGameLog() error {
	info, err := os.Stat(a.config.LogPath)
	if err != nil {
		return err
	}
	if a.knownFile == nil {
		a.knownFile = info
		if time.Since(info.ModTime()) < 30*time.Second {
			a.startRun("recent_log")
			a.offset = 0
		} else {
			a.offset = info.Size()
		}
	}
	if !os.SameFile(a.knownFile, info) || info.Size() < a.offset {
		a.knownFile = info
		a.offset = 0
		a.partial = nil
		a.startRun("log_replaced")
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
				a.startRun("recognized_log")
			}
			a.queue(item)
			if item.Type == "lifecycle" && item.State == "log_closed" && a.active {
				a.queue(event{At: item.At, Type: "session_end", State: "log_closed"})
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
				a.startRun("probe_event")
			}
			a.queue(item)
		}
	}
	return nil
}

func (a *agentState) pollProtonLog() error {
	if a.config.ProtonLogPath == "" {
		return nil
	}
	if len(a.pending) >= maxPendingEvents-maxEvents {
		return nil
	}
	info, err := os.Stat(a.config.ProtonLogPath)
	if err != nil {
		return err
	}
	if a.protonFile == nil {
		a.protonFile = info
		if time.Since(info.ModTime()) >= 30*time.Second {
			a.protonOffset = info.Size()
			return nil
		}
	}
	if !os.SameFile(a.protonFile, info) || info.Size() < a.protonOffset {
		a.protonFile = info
		a.protonOffset = 0
		a.protonPartial = nil
	}
	if info.Size() == a.protonOffset {
		return nil
	}
	file, err := os.Open(a.config.ProtonLogPath)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Seek(a.protonOffset, io.SeekStart); err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(file, maxProtonReadBytes))
	if err != nil {
		return err
	}
	a.protonOffset += int64(len(data))
	data = append(a.protonPartial, data...)
	lines := bytes.Split(data, []byte{'\n'})
	a.protonPartial = append([]byte(nil), lines[len(lines)-1]...)
	for _, line := range lines[:len(lines)-1] {
		if item, ok := parseProtonLine(string(line)); ok {
			if a.runID == "" {
				a.startRun("proton_probe")
			}
			a.queue(item)
		}
	}
	return nil
}

func (a *agentState) startRun(reason string) {
	if a.runID != "" {
		if a.active {
			a.queue(event{At: now(), Type: "session_end", State: "new_log"})
		}
		for len(a.pending) > 0 {
			if err := a.flush(); err != nil {
				log.Printf("previous run final upload: %v", err)
				a.pending = nil
				break
			}
		}
	}
	if reason == "recent_log" || reason == "log_replaced" || reason == "recognized_log" {
		a.protonFile = nil
		a.protonOffset = 0
		a.protonPartial = nil
	}
	a.runID = newRunID()
	a.active = true
	a.lastLiveness = time.Now()
	a.lastProcessSample = time.Time{}
	a.processRuntimeSent = false
	a.queue(event{At: now(), Type: "session_start", State: reason})
	if a.gameRunning {
		a.queue(event{At: now(), Type: "process_state", State: "running"})
	}
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
	for _, item := range steamAntiCheatHashes() {
		a.queue(item)
	}
	for _, item := range platformProfile() {
		a.queue(item)
	}
}

func (a *agentState) pollGameProcess() {
	snapshot := inspectGameProcess()
	running := snapshot.Running
	if !a.processKnown {
		a.processKnown = true
		a.gameRunning = running
		if running && a.runID != "" {
			a.queue(event{At: now(), Type: "process_state", State: "running"})
		}
	} else if running != a.gameRunning {
		a.gameRunning = running
		if a.runID != "" {
			state := "stopped"
			if running {
				state = "running"
				a.processRuntimeSent = false
			}
			a.queue(event{At: now(), Type: "process_state", State: state})
			if !running && a.active {
				a.queue(event{At: now(), Type: "session_end", State: "process_exit"})
				a.active = false
			}
		}
	}
	if running && a.runID != "" && a.active && !a.processRuntimeSent {
		stamp := now()
		if snapshot.OSName != "" {
			a.queue(event{At: stamp, Type: "process_runtime", Name: snapshot.OSName, Version: snapshot.OSVersion})
		}
		if snapshot.Kernel != "" {
			a.queue(event{At: stamp, Type: "process_runtime", Name: "kernel", Version: snapshot.Kernel})
		}
		a.processRuntimeSent = snapshot.OSName != "" || snapshot.Kernel != ""
	}
	if !running || a.runID == "" || !a.active || time.Since(a.lastProcessSample) < 5*time.Second {
		return
	}
	a.lastProcessSample = time.Now()
	a.queue(event{At: now(), Type: "process_sample", State: "running", Threads: snapshot.Threads, FDs: snapshot.FDs, Sockets: snapshot.Sockets})
	for _, name := range observedEnvironmentKeys {
		value := snapshot.Env[name]
		a.queue(event{At: now(), Type: "env_flag", Name: name, Value: &value})
	}
}

type processSnapshot struct {
	Running   bool
	Threads   int64
	FDs       int64
	Sockets   int64
	OSName    string
	OSVersion string
	Kernel    string
	Env       map[string]bool
}

func inspectGameProcess() processSnapshot {
	snapshot := processSnapshot{Env: make(map[string]bool)}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return snapshot
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		commandLine := readSmallFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if strings.Contains(commandLine, "WRFrontiers-Win64-Shipping.exe") {
			snapshot.Running = true
			root := filepath.Join("/proc", entry.Name(), "root")
			snapshot.OSName, snapshot.OSVersion = osReleaseAt(filepath.Join(root, "etc/os-release"))
			snapshot.Kernel = safeVersion(readSmallFile(filepath.Join(root, "proc/sys/kernel/osrelease")))
			if tasks, err := os.ReadDir(filepath.Join("/proc", entry.Name(), "task")); err == nil {
				snapshot.Threads = int64(len(tasks))
			}
			if descriptors, err := os.ReadDir(filepath.Join("/proc", entry.Name(), "fd")); err == nil {
				snapshot.FDs = int64(len(descriptors))
				for _, descriptor := range descriptors {
					target, err := os.Readlink(filepath.Join("/proc", entry.Name(), "fd", descriptor.Name()))
					if err == nil && strings.HasPrefix(target, "socket:[") {
						snapshot.Sockets++
					}
				}
			}
			environment, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "environ"))
			for _, value := range bytes.Split(environment, []byte{0}) {
				key, _, ok := bytes.Cut(value, []byte{'='})
				if !ok {
					continue
				}
				for _, name := range observedEnvironmentKeys {
					if string(key) == name {
						snapshot.Env[name] = true
						break
					}
				}
			}
			return snapshot
		}
	}
	return snapshot
}

func (a *agentState) queue(item event) {
	if item.At == "" {
		item.At = now()
	}
	if validateEvent(&item) != nil {
		return
	}
	a.pending = append(a.pending, item)
	if len(a.pending) > maxPendingEvents {
		a.pending = append([]event(nil), a.pending[len(a.pending)-maxPendingEvents:]...)
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
	var body []byte
	for count > 0 {
		envelope := envelope{
			Schema: schemaVersion, RunID: a.runID, DeviceLabel: a.config.DeviceLabel,
			AgentVersion: safeVersion(version), Mode: a.config.Mode,
			Events: append([]event(nil), a.pending[:count]...),
		}
		var err error
		body, err = json.Marshal(&envelope)
		if err != nil {
			return err
		}
		if len(body) <= maxBodyBytes {
			break
		}
		count--
	}
	if count == 0 {
		return errors.New("event exceeds upload limit")
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

func parseProtonLine(line string) (event, bool) {
	marker := strings.Index(line, "WRFPROBE ")
	if marker < 0 {
		return event{}, false
	}
	fields := make(map[string]string)
	for _, field := range strings.Fields(line[marker+len("WRFPROBE "):]) {
		if name, value, ok := strings.Cut(field, "="); ok {
			fields[name] = value
		}
	}
	switch fields["type"] {
	case "module", "thread", "tls_callback", "dllmain", "exception":
	default:
		return event{}, false
	}
	ticks, err := strconv.ParseInt(fields["filetime"], 10, 64)
	if err != nil || ticks < filetimeUnixOffset {
		return event{}, false
	}
	unixTicks := ticks - filetimeUnixOffset
	item := event{
		At:       time.Unix(unixTicks/10_000_000, unixTicks%10_000_000*100).UTC().Format(time.RFC3339Nano),
		Type:     fields["type"],
		State:    fields["state"],
		Module:   "acclient64.dll",
		RVA:      strings.ToLower(fields["rva"]),
		ThreadID: strings.ToLower(fields["tid"]),
		Reason:   strings.ToLower(fields["reason"]),
		Base:     strings.ToLower(fields["base"]),
		Target:   strings.ToLower(fields["target"]),
	}
	parseNumber := func(name string, maximum uint64) (int64, bool) {
		value, err := strconv.ParseUint(fields[name], 0, 64)
		return int64(value), err == nil && value <= maximum
	}
	for name, destination := range map[string]*int64{
		"code": &item.Code, "flags": &item.Flags, "parameters": &item.Parameters,
		"status": &item.Status, "size": &item.Size,
	} {
		if fields[name] == "" {
			continue
		}
		value, ok := parseNumber(name, 1<<32)
		if !ok {
			return event{}, false
		}
		*destination = value
	}
	if fields["access"] != "" {
		access, ok := parseNumber("access", 8)
		if !ok {
			return event{}, false
		}
		item.Access = &access
	}
	if fields["retval"] != "" {
		if fields["retval"] != "0" && fields["retval"] != "1" {
			return event{}, false
		}
		result := fields["retval"] == "1"
		item.Result = &result
	}
	if validateEvent(&item) != nil {
		return event{}, false
	}
	return item, true
}

func parseGameLine(line string) (event, bool) {
	stamp := eventTime(line)
	lower := strings.ToLower(line)
	if match := acPattern.FindStringSubmatch(line); match != nil {
		return event{At: stamp, Type: "ac_state", State: strings.ToLower(match[1])}, true
	}
	if strings.Contains(lower, "acclient:") && strings.Contains(lower, "try to send client request") {
		return event{At: stamp, Type: "mrac", State: "client_request"}, true
	}
	if strings.Contains(lower, "acclient:") && strings.Contains(lower, "failed to send client request") {
		return event{At: stamp, Type: "mrac", State: "failure", Name: "client_request"}, true
	}
	if strings.Contains(lower, "acclient:") {
		if match := responsePattern.FindStringSubmatch(line); match != nil {
			length, _ := strconv.ParseInt(match[1], 10, 64)
			return event{At: stamp, Type: "mrac", State: "client_response", Length: length}, true
		}
	}
	if match := rpcPattern.FindStringSubmatch(line); match != nil {
		code, _ := strconv.ParseInt(match[2], 10, 64)
		state := strings.ToLower(match[1])
		if state == "error" {
			state = "failure"
		}
		name := match[3]
		if strings.Contains(strings.ToLower(name), "mrac") {
			item := event{At: stamp, Type: "mrac", State: state, Name: name, Code: code}
			if strings.EqualFold(name, "FMracServiceWs::ClientRequest") && (state == "call" || state == "response") {
				item.PayloadB64, item.SHA256, item.Length = parseMRACPayload(line, match[0], state)
			}
			return item, true
		}
		return event{At: stamp, Type: "rpc", State: state, Name: name, Code: code}, true
	}
	if match := closePattern.FindStringSubmatch(line); match != nil {
		code, _ := strconv.ParseInt(match[1], 10, 64)
		return event{At: stamp, Type: "backend_close", Code: code}, true
	}
	if strings.Contains(lower, "mounting project plugin mrac") {
		return event{At: stamp, Type: "mrac", State: "plugin_loaded"}, true
	}
	if match := verbosityPattern.FindStringSubmatch(line); match != nil {
		return event{At: stamp, Type: "diagnostic", Name: match[1], State: strings.ToLower(match[2])}, true
	}
	if strings.Contains(lower, "fmracservicews::clientrequest") || strings.Contains(lower, "mrac:") {
		state := "observed"
		if strings.Contains(lower, "fail") || strings.Contains(lower, "error") {
			state = "failure"
		} else if strings.Contains(lower, "response") || strings.Contains(lower, "received") {
			state = "response"
		} else if strings.Contains(lower, "call") || strings.Contains(lower, "request") {
			state = "call"
		}
		return event{At: stamp, Type: "mrac", State: state}, true
	}
	if strings.Contains(lower, "connection to wss://") && strings.Contains(lower, " established") {
		return event{At: stamp, Type: "backend_state", State: "connected"}, true
	}
	if match := sendPattern.FindStringSubmatch(line); match != nil {
		length, _ := strconv.ParseInt(match[1], 10, 64)
		return event{At: stamp, Type: "transport", State: "send", Length: length}, true
	}
	if match := receivePattern.FindStringSubmatch(line); match != nil {
		code, _ := strconv.ParseInt(match[1], 10, 64)
		return event{At: stamp, Type: "transport", State: "receive", Code: code}, true
	}
	if match := achPattern.FindStringSubmatch(line); match != nil {
		code, _ := strconv.ParseInt(match[1], 10, 64)
		return event{At: stamp, Type: "ach", Code: code}, true
	}
	if match := messagePattern.FindStringSubmatch(line); match != nil {
		code, _ := strconv.ParseInt(match[1], 10, 64)
		return event{At: stamp, Type: "backend_message", Code: code}, true
	}
	for marker, state := range map[string]string{
		"log file open,":                    "log_open",
		"display: game engine initialized.": "engine_initialized",
		"display: starting game.":           "game_starting",
		"engine is initialized. leaving":    "engine_ready",
		"engine exit requested":             "exit_requested",
		"display: preexit game.":            "pre_exit",
		"logexit: game engine shut down":    "engine_shutdown",
		"logexit: exiting.":                 "engine_exiting",
		"log file closed,":                  "log_closed",
	} {
		if strings.Contains(lower, marker) {
			return event{At: stamp, Type: "lifecycle", State: state}, true
		}
	}
	return event{}, false
}

func parseMRACPayload(line, rpcPrefix, state string) (string, string, int64) {
	start := strings.Index(line, rpcPrefix)
	if start < 0 {
		return "", "", 0
	}
	remainder := line[start+len(rpcPrefix):]
	separator := strings.IndexByte(remainder, ':')
	if separator < 0 {
		return "", "", 0
	}
	body := strings.TrimSpace(remainder[separator+1:])
	if len(body) < 2 || body[0] != '\'' || body[len(body)-1] != '\'' ||
		len(body) > base64.StdEncoding.EncodedLen(maxMRACPayloadBytes)+512 {
		return "", "", 0
	}
	body = body[1 : len(body)-1]
	var message struct {
		Value string `json:"value"`
		Data  struct {
			Value string `json:"value"`
		} `json:"data"`
		Response struct {
			Value string `json:"value"`
		} `json:"response"`
	}
	if json.Unmarshal([]byte(body), &message) != nil {
		return "", "", 0
	}
	payloadB64 := message.Value
	if state == "call" && message.Data.Value != "" {
		payloadB64 = message.Data.Value
	} else if state == "response" && message.Response.Value != "" {
		payloadB64 = message.Response.Value
	}
	payload, err := base64.StdEncoding.Strict().DecodeString(payloadB64)
	if err != nil || len(payload) == 0 || len(payload) > maxMRACPayloadBytes {
		return "", "", 0
	}
	digest := sha256.Sum256(payload)
	return payloadB64, hex.EncodeToString(digest[:]), int64(len(payload))
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
	return osReleaseAt("/etc/os-release")
}

func osReleaseAt(path string) (string, string) {
	data := readSmallFile(path)
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

func steamAntiCheatHashes() []event {
	dir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir = filepath.Join(dir, ".local/share/Steam/steamapps/common/WRFrontiers/13_2017027/WRFrontiers/Binaries/Win64")
	var events []event
	for _, name := range []string{"acclient64.dll", "aclaunchapi64.dll"} {
		if digest := fileSHA256(filepath.Join(dir, name)); digest != "" {
			events = append(events, event{At: now(), Type: "diagnostic", State: "sha256", Name: name, Version: digest})
		}
	}
	return events
}

var platformDMIFields = []string{
	"bios_date", "bios_vendor", "bios_version",
	"product_family", "product_name", "product_sku", "product_version", "sys_vendor",
	"chassis_type", "chassis_vendor", "chassis_version",
	"board_name", "board_vendor", "board_version",
}

func platformProfile() []event {
	home, _ := os.UserHomeDir()
	return platformProfileAt(
		"/sys/class/dmi/id",
		"/sys/firmware/acpi/tables",
		"/dev",
		filepath.Join(home, ".local/share/Steam/steamapps/compatdata/1491000/pfx/dosdevices"),
		"/sys/class/drm",
		"/etc/machine-id",
	)
}

func platformProfileAt(dmiDir, acpiDir, devDir, dosDevicesDir, drmDir, machineIDPath string) []event {
	stamp := now()
	var events []event
	for _, name := range platformDMIFields {
		if value := safePlatformText(readSmallFile(filepath.Join(dmiDir, name))); value != "" {
			events = append(events, event{At: stamp, Type: "platform_profile", State: "dmi", Name: name, Text: value})
		}
	}
	for _, name := range []string{"product_serial", "chassis_serial", "board_serial"} {
		present := readSmallFile(filepath.Join(dmiDir, name)) != ""
		events = append(events, event{At: stamp, Type: "platform_profile", State: "identity_readable", Name: name, Result: &present})
	}
	for _, name := range []string{"TPM2", "DMAR", "IVRS"} {
		info, err := os.Stat(filepath.Join(acpiDir, name))
		present := err == nil && info.Mode().IsRegular()
		var size int64
		if present {
			size = info.Size()
		}
		events = append(events, event{At: stamp, Type: "platform_profile", State: "acpi", Name: name, Result: &present, Size: size})
	}
	for _, item := range []struct{ name, path string }{
		{"tpm0", filepath.Join(devDir, "tpm0")},
		{"tpmrm0", filepath.Join(devDir, "tpmrm0")},
		{"physicaldrive0", filepath.Join(dosDevicesDir, "physicaldrive0")},
		{"nvadmindevice", filepath.Join(dosDevicesDir, "nvadmindevice")},
		{"machine_id", machineIDPath},
	} {
		_, err := os.Lstat(item.path)
		present := err == nil
		events = append(events, event{At: stamp, Type: "platform_profile", State: "device", Name: item.name, Result: &present})
	}
	events = append(events, event{At: stamp, Type: "platform_profile", State: "cpu", Name: "logical_cpus", Size: int64(runtime.NumCPU())})

	cards, _ := filepath.Glob(filepath.Join(drmDir, "card[0-9]*"))
	sort.Strings(cards)
	if len(cards) > 8 {
		cards = cards[:8]
	}
	for _, card := range cards {
		cardName := filepath.Base(card)
		for _, property := range []string{"vendor", "device", "subsystem_vendor", "subsystem_device"} {
			if value := safeVersion(readSmallFile(filepath.Join(card, "device", property))); value != "" {
				events = append(events, event{At: stamp, Type: "platform_profile", State: "pci", Name: cardName + "." + property, Version: value})
			}
		}
	}
	return events
}

func safePlatformText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var result strings.Builder
	for _, character := range value {
		if result.Len() >= 128 {
			break
		}
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune(" ._:+/()#-", character) {
			result.WriteRune(character)
		} else {
			result.WriteByte('_')
		}
	}
	return result.String()
}

func fileSHA256(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() > maxDiagnosticBinaryBytes {
		return ""
	}
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, maxDiagnosticBinaryBytes+1))
	if err != nil || written > maxDiagnosticBinaryBytes {
		return ""
	}
	return hex.EncodeToString(digest.Sum(nil))
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
