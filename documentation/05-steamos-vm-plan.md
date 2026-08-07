# SteamOS/QEMU experiment plan

## Short answer

QEMU with KVM and OVMF can boot SteamOS and reproduce much of its userspace:
Steam, Gamescope/Game Mode, Steam Linux Runtime, Proton, and the filesystem and
service layout. It cannot faithfully emulate a Steam Deck.

OVMF supplies generic virtual-machine UEFI firmware. QEMU supplies virtual
ACPI/SMBIOS, chipset, storage, and network devices. With CPU host passthrough the
guest sees the Ryzen 7 7800X3D feature set, not the Deck's Van Gogh APU. A
passed-through RTX 4080 remains an RTX 4080; a virtual GPU is not the Deck's
RDNA2 GPU. A virtual TPM has fresh VM-owned state, not Deck identity.

Valve explicitly says a VM is not recommended for Steam Deck development except
for UI testing, and that a retail Deck uses the same test methods as a devkit.
For this investigation the VM is still useful as a **SteamOS-userspace A/B**,
not as evidence that the service sees a Steam Deck.

## What each outcome would mean

| VM result | Interpretation |
| --- | --- |
| SteamOS does not boot or Steam cannot enter Game Mode | VM/device-model issue; says nothing about WRF |
| WRF chooses a non-118 route | SteamOS userspace alone does not reproduce the route under this VM configuration |
| WRF reaches ACH 118, then closes near 64 s | Stronger evidence that real/supported hardware or another platform property is required |
| WRF remains connected | A SteamOS userspace/session difference matters; compare services, environment, runtime, and launch job before concluding why |
| Real Deck works while VM and desktop fail | Strong evidence for hardware/firmware/platform-specific validation |
| Real Deck also fails | Likely game/service regression or broader Linux route issue; report controlled evidence to support |

No result justifies fabricating DMI, ACPI, TPM, or device identifiers.

## Host readiness snapshot (2026-08-07)

- AMD-V available; `kvm_amd` loaded.
- `/dev/kvm` exists and is accessible.
- 28 IOMMU groups exist.
- RTX 4080 SUPER display and audio functions share isolated group 12.
- Ryzen 7800X3D integrated GPU is separately isolated in group 22.
- KDE currently renders on the RTX; the AMD iGPU has no connected display and
  is the lower-disruption passthrough candidate if a motherboard display output
  can be connected.
- About 532 GiB was free on `/home`.
- QEMU, OVMF, libvirt, virt-install, and swtpm were not installed.
- Nobara repositories offer QEMU 10.2.2, libvirt 12.0.0, OVMF dated
  2026-05-08, swtpm 0.10.1, and virt-install 5.1.0. A direct-QEMU minimum is 25
  packages, 33 MiB download, and 152 MiB installed; libvirt is unnecessary for
  the first test.
- The kernel command line does not explicitly contain an IOMMU option, although
  IOMMU groups are populated.

This is unusually favorable for later GPU passthrough because the host has two
graphics devices. It does not make passthrough the correct first step.

## Safe phased experiment

### Phase 0: preserve the baseline

1. Keep the existing working desktop and Steam installation unchanged.
2. Record current Steam build, runtime, launch options, ACH, account platform,
   anti-cheat state, and disconnect timing.
3. Accept Valve's SteamOS image license and obtain SteamOS only from Valve's
   official download/support pages. Record its resolved URL, release/version,
   file size, and SHA-256 hash.
4. Use a new directory under `/home`, never a physical disk device.

### Phase 1: boot-only VM

Install the minimum direct-QEMU packages after explicit approval:

```bash
sudo dnf install qemu-system-x86-core qemu-img edk2-ovmf \
  qemu-ui-gtk qemu-device-display-virtio-vga-gl qemu-audio-pipewire
```

Use KVM, a Q35 machine, host CPU passthrough, OVMF, NAT networking, a dedicated
virtual disk, and a copied writable OVMF variables file. Do not attach
`/dev/nvme0n1` or any partition. If SteamOS requires a TPM, create a fresh
software TPM owned by this VM; never pass through the host TPM.

The exact launch command should be generated only after package installation,
because Fedora/Nobara OVMF filenames and the Valve image format must be resolved
from the installed files. Do not copy an assumed internet command that targets
`/dev/sdX`.

On 2026-08-07 Valve's stable `steamdeck-repair-latest.img.bz2` endpoint resolved
to `steamdeck-oobe-repair-20260707.10-3.8.14.img.bz2`, a 3,357,999,306-byte
download. The filename still says `steamdeck`, but Valve's current installation
article also targets other supported/beta-supported devices. The license must
be accepted by the user before downloading.

Phase 1 success means only: SteamOS boots, networking works, Steam signs in, and
Game Mode starts. It is enough to inspect the OS, Steam client environment, and
launcher route without risking GPU reassignment.

### Phase 2: software/virtual-GPU probe

Try installing or locating WRF and launch only far enough to record:

- whether Steam offers the title normally;
- the selected Steam Linux Runtime and Proton build;
- launcher job fields, especially ACH;
- whether anti-cheat initializes before graphics become the blocker.

Expect DX12 performance or Vulkan feature support to be insufficient. A virtual
GPU failure is not an anti-cheat result.

### Phase 3: GPU passthrough only if Phase 2 is informative

Preferred lower-disruption layout on this host:

- keep the host desktop on the RTX 4080;
- assign the otherwise idle AMD iGPU in isolated group 22 to VFIO;
- connect a monitor or controlled display endpoint to the motherboard output;
- verify that SteamOS actually selects that GPU for Gamescope and Proton.

Fallback high-performance layout:

- move the host desktop to the Ryzen iGPU;
- assign both RTX functions in IOMMU group 12 to VFIO;
- pass the RTX GPU and its audio function to the VM;
- keep separate VM storage, OVMF variables, and optional software-TPM state.

This changes host graphics binding and can interrupt the desktop. Before doing
it, verify monitor cabling, firmware primary-display choice, group membership,
reset behavior, and recovery through a text console or SSH. Make one reversible
change at a time. Do not bind the GPU to VFIO in this phase without an explicit
go-ahead and a rollback plan.

### Phase 4: decisive hardware control

Repeat the same current Steam build and launch options on a retail Steam Deck or
another officially Powered by SteamOS device. Record the same minimal fields and
disconnect timing. This is more probative than further attempts to make QEMU
look like Deck hardware.

## Sources

- [Valve Steam Deck FAQ](https://partner.steamgames.com/doc/steamhardware/steamdeck/faq)
  — VM and retail-devkit guidance.
- [Valve: load and run games on Steam Deck and Steam Machine](https://partner.steamgames.com/doc/steamhardware/loadgames?language=english)
  — official SteamOS development flow.
- [Valve SteamOS page](https://store.steampowered.com/steamos/buildyourownsteam)
  — current SteamOS availability and supported-device context.
- [Valve SteamOS installation and repair](https://help.steampowered.com/en/faqs/view/65B4-2AA3-5F37-4227)
  — official image, supported-device list, and destructive installer warning.
- [QEMU x86 CPU model documentation](https://www.qemu.org/docs/master/system/qemu-cpu-models.html)
  — KVM host CPU passthrough behavior.
- [QEMU UEFI variables documentation](https://www.qemu.org/docs/master/devel/uefi-vars.html)
  — pflash/UEFI variable storage.
- [QEMU TPM device documentation](https://www.qemu.org/docs/master/specs/tpm.html)
  — emulated and passthrough TPM models and passthrough cautions.
- [Linux kernel VFIO documentation](https://docs.kernel.org/driver-api/vfio.html)
  — IOMMU groups and device assignment.
