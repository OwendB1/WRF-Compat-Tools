package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	systemDmaGuardPolicyInformation = 202
	bcryptRSAPublicMagic            = 0x31415352
)

type probeResult struct {
	Schema                 int    `json:"schema"`
	DMAAvailable           bool   `json:"dma_available"`
	DMAStatus              uint32 `json:"dma_status"`
	DMAReturnLength        uint32 `json:"dma_return_length"`
	DMAPolicy              *bool  `json:"dma_policy,omitempty"`
	NCryptAvailable        bool   `json:"ncrypt_available"`
	PlatformProviderStatus uint32 `json:"platform_provider_status"`
	EKStatus               uint32 `json:"ek_status"`
	EKLength               uint32 `json:"ek_length"`
	EKKind                 string `json:"ek_kind,omitempty"`
	EKBits                 uint32 `json:"ek_bits,omitempty"`
}

func main() {
	result := probeResult{Schema: 1}
	probeDMA(&result)
	probePlatformProvider(&result)
	encoded, _ := json.Marshal(result)
	if executable, err := os.Executable(); err == nil {
		_ = os.WriteFile(executable+".result.json", append(encoded, '\n'), 0600)
	}
	fmt.Printf("WRFPLATFORM %s\n", encoded)
}

func probeDMA(result *probeResult) {
	dll := syscall.NewLazyDLL("ntdll.dll")
	if dll.Load() != nil {
		return
	}
	query := dll.NewProc("NtQuerySystemInformation")
	if query.Find() != nil {
		return
	}
	result.DMAAvailable = true
	var policy byte
	status, _, _ := query.Call(
		systemDmaGuardPolicyInformation,
		uintptr(unsafe.Pointer(&policy)),
		1,
		uintptr(unsafe.Pointer(&result.DMAReturnLength)),
	)
	result.DMAStatus = uint32(status)
	if result.DMAStatus == 0 {
		value := policy != 0
		result.DMAPolicy = &value
	}
}

func probePlatformProvider(result *probeResult) {
	dll := syscall.NewLazyDLL("ncrypt.dll")
	if dll.Load() != nil {
		return
	}
	openProvider := dll.NewProc("NCryptOpenStorageProvider")
	getProperty := dll.NewProc("NCryptGetProperty")
	freeObject := dll.NewProc("NCryptFreeObject")
	if openProvider.Find() != nil || getProperty.Find() != nil || freeObject.Find() != nil {
		return
	}
	result.NCryptAvailable = true
	providerName, _ := syscall.UTF16PtrFromString("Microsoft Platform Crypto Provider")
	propertyName, _ := syscall.UTF16PtrFromString("PCP_EKPUB")
	var provider uintptr
	status, _, _ := openProvider.Call(
		uintptr(unsafe.Pointer(&provider)),
		uintptr(unsafe.Pointer(providerName)),
		0,
	)
	result.PlatformProviderStatus = uint32(status)
	if result.PlatformProviderStatus != 0 {
		return
	}
	defer freeObject.Call(provider)

	buffer := make([]byte, 1024)
	status, _, _ = getProperty.Call(
		provider,
		uintptr(unsafe.Pointer(propertyName)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
		uintptr(unsafe.Pointer(&result.EKLength)),
		0,
	)
	result.EKStatus = uint32(status)
	if result.EKStatus != 0 || result.EKLength < 24 || result.EKLength > uint32(len(buffer)) {
		return
	}
	blob := buffer[:result.EKLength]
	magic := binary.LittleEndian.Uint32(blob[0:4])
	bits := binary.LittleEndian.Uint32(blob[4:8])
	publicExponent := binary.LittleEndian.Uint32(blob[8:12])
	modulus := binary.LittleEndian.Uint32(blob[12:16])
	prime1 := binary.LittleEndian.Uint32(blob[16:20])
	prime2 := binary.LittleEndian.Uint32(blob[20:24])
	if magic == bcryptRSAPublicMagic && prime1 == 0 && prime2 == 0 &&
		24+publicExponent+modulus == result.EKLength && bits == modulus*8 {
		result.EKKind = "rsa_public"
		result.EKBits = bits
	}
}
