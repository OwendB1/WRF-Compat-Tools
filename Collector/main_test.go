package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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

func TestPollLauncherLogCapturesACHWithoutSourceLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.log")
	const secret = "launcher-account-secret"
	if err := os.WriteFile(path, []byte("Launch AC=1 ACH=118 token="+secret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	state := agentState{
		config: agentConfig{LauncherLogPath: path},
		runID:  newRunID(),
		active: true,
	}
	if err := state.pollLauncherLog(); err != nil || len(state.pending) != 1 {
		t.Fatalf("poll result: pending=%#v err=%v", state.pending, err)
	}
	encoded, err := json.Marshal(state.pending[0])
	if err != nil || state.pending[0].Type != "ach" || state.pending[0].Code != 118 || bytes.Contains(encoded, []byte(secret)) {
		t.Fatalf("normalized ACH event = %s, %v", encoded, err)
	}
}

func TestProtonProbePreservesFaultTarget(t *testing.T) {
	const secret = "must-not-leak"
	line := secret + ` 0638:trace:wrfprobe:dispatch_exception WRFPROBE filetime=134307741813188884 type=exception state=dispatch tid=0638 code=0xc0000005 flags=0 rva=0x93ff8 parameters=2 access=0 target=0x12c28`
	item, ok := parseProtonLine(line)
	if !ok || item.At != "2026-08-09T18:36:21.3188884Z" || item.Type != "exception" ||
		item.Module != "acclient64.dll" || item.ThreadID != "0638" || item.Code != 0xc0000005 ||
		item.RVA != "0x93ff8" || item.Access == nil || *item.Access != 0 || item.Target != "0x12c28" {
		t.Fatalf("unexpected probe event: %#v, %v", item, ok)
	}
	encoded, err := json.Marshal(item)
	if err != nil || !bytes.Contains(encoded, []byte(`"access":0`)) || bytes.Contains(encoded, []byte(secret)) {
		t.Fatalf("normalized probe event = %s, %v", encoded, err)
	}

	callback, ok := parseProtonLine(`03d8:trace:wrfprobe:MODULE_InitDLL WRFPROBE filetime=134307741768325196 type=dllmain state=return tid=03d8 reason=PROCESS_ATTACH rva=0x5bbfd6 status=0 retval=1`)
	if !ok || callback.Reason != "process_attach" || callback.Result == nil || !*callback.Result {
		t.Fatalf("callback event = %#v, %v", callback, ok)
	}
}

func TestProtonProbeTracksDeckProfileWithoutSourceLine(t *testing.T) {
	item, ok := parseProtonLine("private-prefix WRFDECKPROFILE selected=cpu,gpu,os")
	if !ok || item.Type != "runtime" || item.Name != "deck_profile" || item.Version != "cpu+gpu+os" {
		t.Fatalf("unexpected profile event: %#v, %v", item, ok)
	}
	if _, ok := parseProtonLine("WRFDECKPROFILE selected=cpu,unknown"); ok {
		t.Fatal("accepted unknown profile feature")
	}
}

func TestPollProtonLogQueuesProbeEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "steam-1491000.log")
	line := "WRFPROBE filetime=134307741813188884 type=exception state=dispatch tid=0638 code=0xc0000005 flags=0 rva=0x93ff8 parameters=2 access=0 target=0x12c28\n"
	if err := os.WriteFile(path, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	state := agentState{config: agentConfig{ProtonLogPath: path}, runID: newRunID()}
	if err := state.pollProtonLog(); err != nil || len(state.pending) != 1 || state.pending[0].Target != "0x12c28" {
		t.Fatalf("poll result: pending=%#v err=%v", state.pending, err)
	}
}

func TestWindowsPlatformProbeNormalizesOnlyStatusMetadata(t *testing.T) {
	const secret = "unique-tpm-ek-secret"
	output := []byte(secret + "\nWRFPLATFORM " +
		`{"schema":1,"dma_available":true,"dma_status":3221225475,"dma_return_length":0,"ncrypt_available":true,"platform_provider_status":0,"ek_status":2148073511,"ek_length":0}` + "\n")
	events, err := parseWindowsPlatformProbeOutput(output, "11.0-100")
	if err != nil || len(events) != 6 {
		t.Fatalf("platform events = %#v, %v", events, err)
	}
	encoded, err := json.Marshal(events)
	if err != nil || bytes.Contains(encoded, []byte(secret)) {
		t.Fatalf("platform probe leaked source data: %s, %v", encoded, err)
	}
	for _, item := range events {
		if err := validateEvent(&item); err != nil {
			t.Fatalf("invalid platform probe event %#v: %v", item, err)
		}
	}
	summaryEvents := make([]storedEvent, 0, len(events))
	for _, item := range events {
		summaryEvents = append(summaryEvents, storedEvent{Event: item})
	}
	summary := summarize(summaryEvents)
	if summary.DMAGuard != "0xc0000003" || summary.PCPEKPub != "0x80090027" {
		t.Fatalf("failure summary = %#v", summary)
	}

	success := []byte(`{"schema":1,"dma_available":true,"dma_status":0,"dma_return_length":1,"dma_policy":false,"ncrypt_available":true,"platform_provider_status":0,"ek_status":0,"ek_length":283,"ek_kind":"rsa_public","ek_bits":2048}` + "\n")
	events, err = parseWindowsPlatformProbeOutput(success, "11.0-100")
	if err != nil {
		t.Fatal(err)
	}
	summaryEvents = summaryEvents[:0]
	for _, item := range events {
		summaryEvents = append(summaryEvents, storedEvent{Event: item})
	}
	summary = summarize(summaryEvents)
	if summary.DMAGuard != "0x00000000/disabled" || summary.PCPEKPub != "0x00000000/283B/RSA2048" {
		t.Fatalf("success summary = %#v", summary)
	}
}

func TestSteamProtonRuntimeUsesAppConfig(t *testing.T) {
	home := t.TempDir()
	client := filepath.Join(home, ".local/share/Steam")
	data := filepath.Join(client, "steamapps/compatdata/1491000")
	proton := filepath.Join(home, "runtime", "Proton 11")
	for _, directory := range []string{data, filepath.Join(proton, "files/share/fonts")} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(proton, "proton"), []byte("launcher"), 0700); err != nil {
		t.Fatal(err)
	}
	config := "11.0-100\n" + filepath.Join(proton, "files/share/fonts") + "/\n" + client + "\n"
	if err := os.WriteFile(filepath.Join(data, "config_info"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	runtimeInfo, err := steamProtonRuntimeAt(home)
	if err != nil || runtimeInfo.Version != "11.0-100" || runtimeInfo.Root != proton || runtimeInfo.Client != client || runtimeInfo.Data != data {
		t.Fatalf("runtime = %#v, %v", runtimeInfo, err)
	}
}

func TestLegacyInstrumentedConfigBecomesSteamDeckReference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	config := `{"url":"https://collector.test","token":"` + strings.Repeat("a", 64) + `","device_label":"deck-a","mode":"instrumented","log_path":"/tmp/game.log","auto_update":true}`
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadAgentConfig(path)
	if err != nil || loaded.Mode != "steamdeck_reference" {
		t.Fatalf("loaded mode = %q, err = %v", loaded.Mode, err)
	}
}

func TestRunSummaryIncludesProbeSweep(t *testing.T) {
	read := int64(0)
	events := []storedEvent{
		{Event: event{At: "2026-08-09T18:36:21Z", Type: "module", State: "loaded"}},
		{Event: event{At: "2026-08-09T18:36:22Z", Type: "exception", Access: &read, Target: "0x1000"}},
		{Event: event{At: "2026-08-09T18:36:22.01Z", Type: "exception", Access: &read, Target: "0x2000"}},
		{Event: event{At: "2026-08-09T18:36:23Z", Type: "exception", Access: &read, Target: "0x3000"}},
	}
	summary := summarize(events)
	if summary.ProbeEvents != 4 || summary.ProbeFaults != 3 || summary.ProbeTargets != 3 || summary.ProbeSlices != 2 {
		t.Fatalf("probe summary: %#v", summary)
	}
}

func TestParserCapturesCompleteDiagnostics(t *testing.T) {
	requestPayload := base64.StdEncoding.EncodeToString([]byte("complete MRAC request"))
	responsePayload := base64.StdEncoding.EncodeToString([]byte("complete MRAC response"))
	tests := []struct {
		line      string
		eventType string
		state     string
		name      string
		code      int64
		length    int64
		payload   string
	}{
		{
			`[2026.08.07-10.18.06:786][995] GLogBackendRpcWsCalls: Verbose: [RPC Call (48)] FMracServiceWs::ClientRequest :'{"data":{"value":"` + requestPayload + `"}}'`,
			"mrac", "call", "FMracServiceWs::ClientRequest", 48, int64(len("complete MRAC request")), requestPayload,
		},
		{
			`[2026.08.07-10.18.06:886][0] GLogBackendRpcWsCalls: Verbose: [RPC Response (48), traceparent: redacted] FMracServiceWs::ClientRequest : '{"response":{"value":"` + responsePayload + `"}}'`,
			"mrac", "response", "FMracServiceWs::ClientRequest", 48, int64(len("complete MRAC response")), responsePayload,
		},
		{
			`[2026.08.07-10.18.06:886][0] ACClient: Verbose: Received server response (681 bytes)`,
			"mrac", "client_response", "", 0, 681, "",
		},
		{
			`[2026.08.07-10.18.06:887][1] ACClient: Warning: Failed to send client request`,
			"mrac", "failure", "client_request", 0, 0, "",
		},
		{
			`[2026.08.07-10.18.01:283][670] GLogBackendRpcWsCalls: Verbose: [RPC Call (1)] FAuthenticationServiceWs::PlatformAuth : '{}'`,
			"rpc", "call", "FAuthenticationServiceWs::PlatformAuth", 1, 0, "",
		},
		{
			`[2026.08.07-10.18.30:253][669] GLogBackendRpcWsCalls: Verbose: [RPC Response (2)] FSessionProviderServiceWs::Ping : '{}'`,
			"rpc", "response", "FSessionProviderServiceWs::Ping", 2, 0, "",
		},
		{
			`[2026.08.07-10.18.01:253][669] GLogBackendRpcWs: Connection to wss://backend.example/server established`,
			"backend_state", "connected", "", 0, 0, "",
		},
		{
			`[2026.08.07-10.52.15:344][845] Log file closed, 08/07/26 12:52:15`,
			"lifecycle", "log_closed", "", 0, 0, "",
		},
	}
	for _, test := range tests {
		item, ok := parseGameLine(test.line)
		if !ok || item.Type != test.eventType || item.State != test.state || item.Name != test.name || item.Code != test.code || item.Length != test.length || item.PayloadB64 != test.payload {
			t.Errorf("parseGameLine(%q) = %#v, %v", test.line, item, ok)
		}
		if test.payload != "" && (item.SHA256 == "" || validateEvent(&item) != nil) {
			t.Fatalf("MRAC payload lacks valid integrity metadata: %#v", item)
		}
	}
	const unrelatedSecret = "unrelated-private-body"
	item, ok := parseGameLine(`[RPC Call (9)] FAuthenticationServiceWs::PlatformAuth : '{"token":"` + unrelatedSecret + `"}'`)
	encoded, err := json.Marshal(item)
	if !ok || err != nil || bytes.Contains(encoded, []byte(unrelatedSecret)) {
		t.Fatalf("unrelated RPC body leaked: %s", encoded)
	}
	item, ok = parseGameLine(`[RPC Call (10)] FMracServiceWs::ClientRequest : '{"data":{"value":"not-base64"}}'`)
	if !ok || item.PayloadB64 != "" {
		t.Fatalf("invalid MRAC body was retained: %#v", item)
	}
	if item, ok := parseGameLine("ACClient: Verbose: OnLoginComplete UserId=secret"); ok {
		t.Fatalf("unstructured ACClient line became an event: %#v", item)
	}
}

func TestMRACPayloadValidationIsScopedAndBounded(t *testing.T) {
	item, ok := parseGameLine(`[RPC Call (47)] FMracServiceWs::ClientRequest : '{"data":{"value":"aGVsbG8="}}'`)
	if !ok || validateEvent(&item) != nil {
		t.Fatalf("valid MRAC payload rejected: %#v", item)
	}

	wrongType := item
	wrongType.Type = "rpc"
	if validateEvent(&wrongType) == nil {
		t.Fatal("payload accepted on an unrelated RPC event")
	}

	wrongDigest := item
	wrongDigest.SHA256 = strings.Repeat("0", 64)
	if validateEvent(&wrongDigest) == nil {
		t.Fatal("payload with a wrong digest was accepted")
	}

	oversized := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{'x'}, maxMRACPayloadBytes+1))
	item.PayloadB64 = oversized
	item.Length = maxMRACPayloadBytes + 1
	if validateEvent(&item) == nil {
		t.Fatal("oversized MRAC payload was accepted")
	}
}

func TestParserCapturesResponseTraceID(t *testing.T) {
	const traceID = "ab95b51a8e6956884464950d50a9c7f0"
	item, ok := parseGameLine(`[RPC Response (47), traceparent: 00--` + traceID + `--82d171fa48a9073d--01] FMracServiceWs::ClientRequest : '{}'`)
	if !ok || item.Type != "mrac" || item.State != "response" || item.TraceID != traceID || validateEvent(&item) != nil {
		t.Fatalf("MRAC trace ID = %#v, %v", item, ok)
	}

	item, ok = parseGameLine(`[RPC Response (2), traceparent : 00-ABCDEF0123456789ABCDEF0123456789-0123456789abcdef-01] FSessionProviderServiceWs::Ping : '{}'`)
	if !ok || item.Type != "rpc" || item.TraceID != "abcdef0123456789abcdef0123456789" || validateEvent(&item) != nil {
		t.Fatalf("RPC trace ID = %#v, %v", item, ok)
	}

	invalid := event{At: now(), Type: "rpc", State: "call", TraceID: traceID}
	if validateEvent(&invalid) == nil {
		t.Fatal("trace ID accepted on RPC call")
	}
}

func TestRunSummaryIncludesCaptureBounds(t *testing.T) {
	start := "2026-08-07T10:18:00Z"
	end := "2026-08-07T10:19:04Z"
	events := []storedEvent{
		{Event: event{At: start, Type: "session_start", State: "recent_log"}},
		{Event: event{At: "2026-08-07T10:18:00.5Z", Type: "ach", Code: 118}},
		{Event: event{At: "2026-08-07T10:18:01Z", Type: "diagnostic", Name: "GLogBackendRpcWsCalls", State: "veryverbose"}},
		{Event: event{At: "2026-08-07T10:18:05Z", Type: "mrac", State: "client_request"}},
		{Event: event{At: "2026-08-07T10:18:06Z", Type: "mrac", State: "call"}},
		{Event: event{At: "2026-08-07T10:18:07Z", Type: "mrac", State: "response", PayloadB64: "YQ=="}},
		{Event: event{At: "2026-08-07T10:18:07.5Z", Type: "mrac", State: "failure", Name: "client_request"}},
		{Event: event{At: "2026-08-07T10:18:08Z", Type: "rpc", State: "call"}},
		{Event: event{At: "2026-08-07T10:18:09Z", Type: "rpc", State: "response"}},
		{Event: event{At: "2026-08-07T10:18:30Z", Type: "rpc", State: "call", Name: "FSessionProviderServiceWs::Ping"}},
		{Event: event{At: "2026-08-07T10:18:30.1Z", Type: "rpc", State: "response", Name: "FSessionProviderServiceWs::Ping"}},
		{Event: event{At: "2026-08-07T10:18:40Z", Type: "collector_state", State: "resumed", DurationMS: 5000}},
		{Event: event{At: "2026-08-07T10:18:50Z", Type: "gate_result", Length: 0}},
		{Event: event{At: "2026-08-07T10:19:03Z", Type: "backend_close", Code: 1006}},
		{Event: event{At: end, Type: "session_end", State: "process_exit"}},
	}
	summary := summarize(events)
	if summary.Started != start || summary.Ended != end || summary.Duration != "1m4s" {
		t.Fatalf("capture bounds: %#v", summary)
	}
	if !summary.ACHObserved || summary.ACH != 118 {
		t.Fatalf("ACH summary: %#v", summary)
	}
	if !summary.Diagnostics || !summary.MRAC || summary.MRACState != "failure" || summary.MRACAttempts != 1 || summary.MRACCalls != 1 || summary.MRACResponses != 1 || summary.MRACFailures != 1 || summary.RPCCalls != 2 || summary.RPCResponses != 2 {
		t.Fatalf("diagnostic summary: %#v", summary)
	}
	if summary.MRACPayloads != 1 {
		t.Fatalf("MRAC payload summary: %#v", summary)
	}
	if !summary.GateObserved || summary.GateLength != 0 {
		t.Fatalf("gate summary: %#v", summary)
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

func TestPlatformProfileCapturesModelWithoutUniqueIdentity(t *testing.T) {
	root := t.TempDir()
	dmi := filepath.Join(root, "dmi")
	acpi := filepath.Join(root, "acpi")
	dev := filepath.Join(root, "dev")
	dos := filepath.Join(root, "dosdevices")
	drmDevice := filepath.Join(root, "drm", "card0", "device")
	cpuTopology := filepath.Join(root, "cpu", "cpu0", "topology")
	dmiEntry := filepath.Join(root, "dmi-entries", "1-0")
	dmiTables := filepath.Join(root, "dmi-tables")
	disk := filepath.Join(root, "block", "nvme0n1")
	efivars := filepath.Join(root, "efi", "efivars")
	tpm := filepath.Join(root, "tpm", "tpm0")
	for _, dir := range []string{dmi, acpi, dev, dos, drmDevice, cpuTopology, dmiEntry, dmiTables,
		filepath.Join(disk, "device"), filepath.Join(disk, "queue"), efivars, tpm,
		filepath.Join(root, "iommu", "7")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dmi, "sys_vendor"), "Valve\n")
	write(filepath.Join(dmi, "product_name"), "Jupiter™\n")
	write(filepath.Join(dmi, "product_serial"), "unique-product-secret\n")
	write(filepath.Join(dmi, "product_uuid"), "unique-uuid-secret\n")
	write(filepath.Join(acpi, "TPM2"), strings.Repeat("x", 76))
	write(filepath.Join(dev, "tpmrm0"), "")
	write(filepath.Join(root, "machine-id"), "unique-machine-secret\n")
	write(filepath.Join(drmDevice, "vendor"), "0x1002\n")
	write(filepath.Join(drmDevice, "device"), "0x163f\n")
	write(filepath.Join(root, "cpuinfo"), "processor: 0\nvendor_id: AuthenticAMD\ncpu family: 23\nmodel: 145\nmodel name: AMD Custom APU 0405\nstepping: 0\n")
	write(filepath.Join(cpuTopology, "physical_package_id"), "0\n")
	write(filepath.Join(cpuTopology, "core_id"), "0\n")
	write(filepath.Join(dmiEntry, "length"), "27\n")
	rawDMI := "unique-raw-smbios-secret" + strings.Repeat("x", 525-len("unique-raw-smbios-secret"))
	write(filepath.Join(dmiTables, "DMI"), rawDMI)
	write(filepath.Join(dmiTables, "smbios_entry_point"), strings.Repeat("x", 24))
	write(filepath.Join(disk, "device", "vendor"), "NVMe\n")
	write(filepath.Join(disk, "device", "model"), "Deck Drive\n")
	write(filepath.Join(disk, "device", "rev"), "1.0\n")
	write(filepath.Join(disk, "device", "serial"), "unique-storage-secret\n")
	write(filepath.Join(disk, "queue", "logical_block_size"), "512\n")
	write(filepath.Join(disk, "queue", "physical_block_size"), "4096\n")
	write(filepath.Join(disk, "size"), "1000\n")
	write(filepath.Join(disk, "removable"), "0\n")
	write(filepath.Join(disk, "ro"), "0\n")
	write(filepath.Join(efivars, "SecureBoot-test"), "\x00\x00\x00\x00\x01")
	write(filepath.Join(root, "lockdown"), "none [integrity] confidentiality\n")
	write(filepath.Join(tpm, "tpm_version_major"), "2\n")
	write(filepath.Join(tpm, "ek"), "unique-tpm-key-secret\n")
	write(filepath.Join(root, "os-release"), "ID=steamos\nVERSION_ID=3\nVARIANT_ID=steamdeck\nBUILD_ID=20260830\n")
	if err := os.Symlink("/dev/null", filepath.Join(dos, "physicaldrive0")); err != nil {
		t.Fatal(err)
	}

	events := platformProfileAt(platformPaths{
		dmiDir: dmi, dmiEntriesDir: filepath.Join(root, "dmi-entries"), dmiTablesDir: dmiTables, acpiDir: acpi,
		devDir: dev, dosDevicesDir: dos, drmDir: filepath.Join(root, "drm"),
		blockDir: filepath.Join(root, "block"), cpuDir: filepath.Join(root, "cpu"),
		cpuInfoPath: filepath.Join(root, "cpuinfo"), tpmDir: filepath.Join(root, "tpm"),
		efiDir: filepath.Join(root, "efi"), lockdownPath: filepath.Join(root, "lockdown"),
		iommuDir: filepath.Join(root, "iommu"), machineIDPath: filepath.Join(root, "machine-id"),
		osReleasePath: filepath.Join(root, "os-release"),
	})
	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"unique-product-secret", "unique-uuid-secret", "unique-machine-secret", "unique-raw-smbios-secret", "unique-storage-secret", "unique-tpm-key-secret"} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatalf("platform profile leaked %q: %s", secret, encoded)
		}
	}
	found := map[string]event{}
	for _, item := range events {
		if err := validateEvent(&item); err != nil {
			t.Fatalf("invalid platform event %#v: %v", item, err)
		}
		found[item.State+":"+item.Name] = item
	}
	if found["dmi:sys_vendor"].Text != "Valve" || found["dmi:product_name"].Text != "Jupiter_" {
		t.Fatalf("DMI model fields = %#v", found)
	}
	if item := found["identity_readable:product_serial"]; item.Result == nil || !*item.Result {
		t.Fatalf("serial readability = %#v", item)
	}
	if item := found["acpi:TPM2"]; item.Result == nil || !*item.Result || item.Size != 76 {
		t.Fatalf("TPM2 profile = %#v", item)
	}
	if item := found["device:physicaldrive0"]; item.Result == nil || !*item.Result {
		t.Fatalf("physical drive profile = %#v", item)
	}
	if found["pci:card0.vendor"].Version != "0x1002" || found["pci:card0.device"].Version != "0x163f" {
		t.Fatalf("PCI profile = %#v", found)
	}
	if found["cpu:vendor"].Text != "AuthenticAMD" || found["cpu:physical_cores"].Size != 1 {
		t.Fatalf("CPU profile = %#v", found)
	}
	if found["smbios:type_1"].Size != 1 || found["smbios:type_1"].Length != 27 {
		t.Fatalf("SMBIOS shape = %#v", found)
	}
	if found["smbios:raw_table"].Size != 525 {
		t.Fatalf("SMBIOS table metadata = %#v", found)
	}
	if found["storage:disk0.model"].Text != "Deck Drive" || found["storage:disk0.bytes"].Size != 512000 {
		t.Fatalf("storage profile = %#v", found)
	}
	if item := found["firmware:secure_boot_enabled"]; item.Result == nil || !*item.Result || found["firmware:iommu_groups"].Size != 1 {
		t.Fatalf("firmware profile = %#v", found)
	}
	if found["tpm:version_major"].Version != "2" || found["os_release:variant_id"].Version != "steamdeck" {
		t.Fatalf("TPM/OS profile = %#v", found)
	}

	invalid := event{At: now(), Type: "diagnostic", State: "dmi", Text: "Valve"}
	if validateEvent(&invalid) == nil {
		t.Fatal("platform text accepted on unrelated event")
	}
}

func TestFileSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "binary.dll")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := fileSHA256(path); got != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("fileSHA256 = %q", got)
	}
	tooLarge := filepath.Join(t.TempDir(), "too-large.dll")
	if err := os.WriteFile(tooLarge, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(tooLarge, maxDiagnosticBinaryBytes+1); err != nil {
		t.Fatal(err)
	}
	if got := fileSHA256(tooLarge); got != "" {
		t.Fatalf("oversized fileSHA256 = %q", got)
	}
}

func TestIngestRequiresTokenAndStoresCompleteEvent(t *testing.T) {
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
	mrac, ok := parseGameLine(`[RPC Response (47)] FMracServiceWs::ClientRequest : '{"response":{"value":"cmVzcG9uc2U="}}'`)
	if !ok || mrac.PayloadB64 == "" {
		t.Fatal("MRAC fixture did not parse")
	}
	input := envelope{
		Schema: schemaVersion, RunID: newRunID(), DeviceLabel: "deck-a",
		AgentVersion: "test", Mode: "steamdeck_reference",
		Events: []event{
			{At: time.Now().UTC().Format(time.RFC3339Nano), Type: "ac_state", State: "online"},
			mrac,
		},
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
	if !bytes.Contains(data, []byte(`"state":"online"`)) || !bytes.Contains(data, []byte(`"payload_base64":"cmVzcG9uc2U="`)) {
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

func TestFlushKeepsExtensivePayloadBatchWithinServerLimit(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{'x'}, maxMRACPayloadBytes))
	parsed, ok := parseGameLine(`[RPC Call (47)] FMracServiceWs::ClientRequest : '{"data":{"value":"` + payload + `"}}'`)
	if !ok || parsed.PayloadB64 == "" {
		t.Fatal("large valid payload was not parsed")
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.ContentLength > maxBodyBytes {
			t.Errorf("request size = %d", r.ContentLength)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	state := &agentState{
		config: agentConfig{URL: server.URL, Token: strings.Repeat("a", 64), DeviceLabel: "deck-a", Mode: "steamdeck_reference"},
		client: server.Client(), runID: newRunID(), pending: bytesToEvents(parsed, 10),
	}
	if err := state.flush(); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || len(state.pending) == 0 || len(state.pending) >= 10 {
		t.Fatalf("unexpected bounded flush: requests=%d pending=%d", requests, len(state.pending))
	}
}

func bytesToEvents(item event, count int) []event {
	items := make([]event, count)
	for i := range items {
		items[i] = item
	}
	return items
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
	if !strings.Contains(response.Body.String(), "complete Base64 MRAC ClientRequest") || !strings.Contains(response.Body.String(), "Data notice") {
		t.Fatal("landing page did not disclose permanent extensive capture")
	}
	if !strings.Contains(response.Body.String(), "fault target addresses") {
		t.Fatal("landing page did not disclose Proton fault-target capture")
	}

	response = httptest.NewRecorder()
	server.installerHandler(response, httptest.NewRequest(http.MethodGet, "/download/install.sh", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "One-time enrollment code") {
		t.Fatalf("installer response: %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), `collector_url="https://collector.test"`) {
		t.Fatal("downloaded installer did not receive the configured public URL")
	}
	if !strings.Contains(response.Body.String(), "WRF COLLECTOR DIAGNOSTICS") || !strings.Contains(response.Body.String(), `"mode":"steamdeck_reference"`) {
		t.Fatal("downloaded installer did not label the Steam Deck reference route")
	}
	if !strings.Contains(response.Body.String(), "complete Base64 MRAC ClientRequest") || !strings.Contains(response.Body.String(), "GLogBackendRpcProtobuf=VeryVerbose") ||
		!strings.Contains(response.Body.String(), `"proton_log_path"`) || !strings.Contains(response.Body.String(), "fault target addresses") {
		t.Fatal("downloaded installer did not enable and disclose complete diagnostics")
	}
	if !strings.Contains(response.Body.String(), "--connect-timeout 10 --max-time 120") || !strings.Contains(response.Body.String(), "Downloading collector agent...") {
		t.Fatal("downloaded installer did not expose bounded downloads")
	}

	response = httptest.NewRecorder()
	server.dashboard(response, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("admin status = %d", response.Code)
	}

	runID := newRunID()
	if err := server.appendEvents(&envelope{
		RunID: runID, DeviceLabel: "deck-a", AgentVersion: "sha-123456789abc", Mode: "steamdeck_reference",
		Events: []event{{At: time.Now().UTC().Format(time.RFC3339Nano), Type: "session_start", State: "new_log"}},
	}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request.SetBasicAuth("admin", server.adminToken)
	response = httptest.NewRecorder()
	server.dashboard(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "sha-123456789abc") || !strings.Contains(response.Body.String(), "steamdeck_reference") || !strings.Contains(response.Body.String(), "/admin/runs/delete") {
		t.Fatalf("dashboard did not identify the run build and route: %d %s", response.Code, response.Body.String())
	}
}

func TestAdminDeletesOnlySelectedRun(t *testing.T) {
	server := &server{dataDir: t.TempDir(), adminToken: strings.Repeat("b", 32)}
	runIDs := []string{newRunID(), newRunID()}
	for _, runID := range runIDs {
		if err := server.appendEvents(&envelope{
			RunID: runID, DeviceLabel: "deck-a", AgentVersion: "test", Mode: "steamdeck_reference",
			Events: []event{{At: time.Now().UTC().Format(time.RFC3339Nano), Type: "session_end", State: "process_exit"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	confirmation := httptest.NewRequest(http.MethodGet, "/admin/runs/delete?run_id="+runIDs[0], nil)
	confirmation.SetBasicAuth("admin", server.adminToken)
	response := httptest.NewRecorder()
	server.deleteRunHandler(response, confirmation)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Permanently delete") || !strings.Contains(response.Body.String(), runIDs[0]) {
		t.Fatalf("delete confirmation = %d: %s", response.Code, response.Body.String())
	}
	request := func(token string) *http.Request {
		form := url.Values{"run_id": {runIDs[0]}, "csrf_token": {token}}
		result := httptest.NewRequest(http.MethodPost, "/admin/runs/delete", strings.NewReader(form.Encode()))
		result.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		result.SetBasicAuth("admin", server.adminToken)
		return result
	}

	response = httptest.NewRecorder()
	server.deleteRunHandler(response, request("wrong"))
	if response.Code != http.StatusForbidden {
		t.Fatalf("invalid CSRF status = %d", response.Code)
	}

	response = httptest.NewRecorder()
	server.deleteRunHandler(response, request(server.csrfToken()))
	if response.Code != http.StatusSeeOther {
		t.Fatalf("delete status = %d: %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(server.dataDir, runIDs[0]+".jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("selected run still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(server.dataDir, runIDs[1]+".jsonl")); err != nil {
		t.Fatalf("unselected run was removed: %v", err)
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
