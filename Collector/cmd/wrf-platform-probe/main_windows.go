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
	systemCpuInformation            = 1
	systemFirmwareTableInformation  = 76
	bcryptRSAPublicMagic            = 0x31415352
	firmwareProviderRSMB            = 0x52534d42
	firmwareTableGet                = 1
	ioctlVolumeDiskExtents          = 0x00560000
	ioctlStorageQueryProperty       = 0x002d1400
)

type smbiosStructure struct {
	Type        uint8  `json:"type"`
	Length      uint8  `json:"length"`
	Strings     uint32 `json:"strings"`
	StringBytes uint32 `json:"string_bytes"`
}

type probeResult struct {
	Schema                 int               `json:"schema"`
	DMAAvailable           bool              `json:"dma_available"`
	DMAStatus              uint32            `json:"dma_status"`
	DMAReturnLength        uint32            `json:"dma_return_length"`
	DMAPolicy              *bool             `json:"dma_policy,omitempty"`
	NCryptAvailable        bool              `json:"ncrypt_available"`
	PlatformProviderStatus uint32            `json:"platform_provider_status"`
	EKStatus               uint32            `json:"ek_status"`
	EKLength               uint32            `json:"ek_length"`
	EKKind                 string            `json:"ek_kind,omitempty"`
	EKBits                 uint32            `json:"ek_bits,omitempty"`
	CPUAvailable           bool              `json:"cpu_available"`
	CPUStatus              uint32            `json:"cpu_status"`
	CPUArchitecture        uint16            `json:"cpu_architecture"`
	CPULevel               uint16            `json:"cpu_level"`
	CPURevision            uint16            `json:"cpu_revision"`
	CPUMaximumProcessors   uint16            `json:"cpu_maximum_processors"`
	CPUFeatureBits         uint32            `json:"cpu_feature_bits"`
	RSMBAvailable          bool              `json:"rsmb_available"`
	RSMBStatus             uint32            `json:"rsmb_status"`
	RSMBLength             uint32            `json:"rsmb_length"`
	SMBIOSMajor            uint8             `json:"smbios_major"`
	SMBIOSMinor            uint8             `json:"smbios_minor"`
	SMBIOSLength           uint32            `json:"smbios_length"`
	SMBIOSStructures       []smbiosStructure `json:"smbios_structures,omitempty"`
	VolumeAvailable        bool              `json:"volume_available"`
	VolumeStatus           uint32            `json:"volume_status"`
	VolumeExtentCount      uint32            `json:"volume_extent_count"`
	VolumeDiskNumber       uint32            `json:"volume_disk_number"`
	VolumeStartsAtZero     bool              `json:"volume_starts_at_zero"`
	StorageAvailable       bool              `json:"storage_available"`
	StorageStatus          uint32            `json:"storage_status"`
	StorageDescriptorSize  uint32            `json:"storage_descriptor_size"`
	StorageBusType         uint32            `json:"storage_bus_type"`
	StorageIdentityOffsets uint32            `json:"storage_identity_offsets"`
}

func main() {
	result := probeResult{Schema: 1}
	probeDMA(&result)
	probePlatformProvider(&result)
	probeCPUAndRSMB(&result)
	probeStorage(&result)
	encoded, _ := json.Marshal(result)
	if executable, err := os.Executable(); err == nil {
		_ = os.WriteFile(executable+".result.json", append(encoded, '\n'), 0600)
	}
	fmt.Printf("WRFPLATFORM %s\n", encoded)
}

func probeCPUAndRSMB(result *probeResult) {
	dll := syscall.NewLazyDLL("ntdll.dll")
	if dll.Load() != nil {
		return
	}
	query := dll.NewProc("NtQuerySystemInformation")
	if query.Find() != nil {
		return
	}

	result.CPUAvailable = true
	cpu := make([]byte, 12)
	status, _, _ := query.Call(systemCpuInformation, uintptr(unsafe.Pointer(&cpu[0])), uintptr(len(cpu)), 0)
	result.CPUStatus = uint32(status)
	if result.CPUStatus == 0 {
		result.CPUArchitecture = binary.LittleEndian.Uint16(cpu[0:2])
		result.CPULevel = binary.LittleEndian.Uint16(cpu[2:4])
		result.CPURevision = binary.LittleEndian.Uint16(cpu[4:6])
		result.CPUMaximumProcessors = binary.LittleEndian.Uint16(cpu[6:8])
		result.CPUFeatureBits = binary.LittleEndian.Uint32(cpu[8:12])
	}

	result.RSMBAvailable = true
	buffer := make([]byte, 64*1024)
	binary.LittleEndian.PutUint32(buffer[0:4], firmwareProviderRSMB)
	binary.LittleEndian.PutUint32(buffer[4:8], firmwareTableGet)
	status, _, _ = query.Call(systemFirmwareTableInformation, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), 0)
	result.RSMBStatus = uint32(status)
	if result.RSMBStatus != 0 {
		return
	}
	result.RSMBLength = binary.LittleEndian.Uint32(buffer[12:16])
	if result.RSMBLength < 8 || result.RSMBLength > uint32(len(buffer)-16) {
		return
	}
	table := buffer[16 : 16+result.RSMBLength]
	result.SMBIOSMajor = table[1]
	result.SMBIOSMinor = table[2]
	result.SMBIOSLength = binary.LittleEndian.Uint32(table[4:8])
	if result.SMBIOSLength > uint32(len(table)-8) {
		return
	}
	for offset, limit := 8, 8+int(result.SMBIOSLength); offset+4 <= limit; {
		kind, length := table[offset], int(table[offset+1])
		if length < 4 || offset+length > limit {
			break
		}
		stringsStart, stringsEnd := offset+length, offset+length
		for stringsEnd+1 < limit && (table[stringsEnd] != 0 || table[stringsEnd+1] != 0) {
			stringsEnd++
		}
		if stringsEnd+1 >= limit {
			break
		}
		var count, bytes uint32
		for cursor := stringsStart; cursor < stringsEnd; {
			end := cursor
			for end < stringsEnd && table[end] != 0 {
				end++
			}
			count++
			bytes += uint32(end - cursor)
			cursor = end + 1
		}
		result.SMBIOSStructures = append(result.SMBIOSStructures, smbiosStructure{
			Type: kind, Length: uint8(length), Strings: count, StringBytes: bytes,
		})
		offset = stringsEnd + 2
		if kind == 127 {
			break
		}
	}
}

func probeStorage(result *probeResult) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	createFile := kernel32.NewProc("CreateFileW")
	deviceIoControl := kernel32.NewProc("DeviceIoControl")
	closeHandle := kernel32.NewProc("CloseHandle")
	if kernel32.Load() != nil || createFile.Find() != nil || deviceIoControl.Find() != nil || closeHandle.Find() != nil {
		return
	}
	open := func(path string) uintptr {
		name, _ := syscall.UTF16PtrFromString(path)
		handle, _, _ := createFile.Call(uintptr(unsafe.Pointer(name)), 0, 3, 0, 3, 0, 0)
		return handle
	}
	call := func(handle uintptr, code uint32, input, output []byte) (uint32, uint32) {
		var inputPointer, outputPointer uintptr
		if len(input) != 0 {
			inputPointer = uintptr(unsafe.Pointer(&input[0]))
		}
		if len(output) != 0 {
			outputPointer = uintptr(unsafe.Pointer(&output[0]))
		}
		var returned uint32
		ok, _, err := deviceIoControl.Call(handle, uintptr(code), inputPointer, uintptr(len(input)),
			outputPointer, uintptr(len(output)), uintptr(unsafe.Pointer(&returned)), 0)
		if ok == 0 {
			if errno, ok := err.(syscall.Errno); ok {
				return uint32(errno), returned
			}
			return 1, returned
		}
		return 0, returned
	}

	invalid := ^uintptr(0)
	if handle := open(`\\.\C:`); handle != invalid {
		result.VolumeAvailable = true
		defer closeHandle.Call(handle)
		output := make([]byte, 32)
		result.VolumeStatus, _ = call(handle, ioctlVolumeDiskExtents, nil, output)
		if result.VolumeStatus == 0 {
			result.VolumeExtentCount = binary.LittleEndian.Uint32(output[0:4])
			result.VolumeDiskNumber = binary.LittleEndian.Uint32(output[8:12])
			result.VolumeStartsAtZero = binary.LittleEndian.Uint64(output[16:24]) == 0
		}
	}
	if handle := open(`\\.\PhysicalDrive0`); handle != invalid {
		result.StorageAvailable = true
		defer closeHandle.Call(handle)
		input, output := make([]byte, 12), make([]byte, 64)
		result.StorageStatus, _ = call(handle, ioctlStorageQueryProperty, input, output)
		if result.StorageStatus == 0 {
			result.StorageDescriptorSize = binary.LittleEndian.Uint32(output[4:8])
			result.StorageBusType = binary.LittleEndian.Uint32(output[28:32])
			for index, offset := range []int{12, 16, 20, 24} {
				if binary.LittleEndian.Uint32(output[offset:offset+4]) != 0 {
					result.StorageIdentityOffsets |= 1 << index
				}
			}
		}
	}
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
