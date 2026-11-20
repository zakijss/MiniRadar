<p align="center"><img src="docs/hero.jpg" alt="A grey Miyoo Mini Plus on a stand on a sunlit window sill, listing network devices in MiniRadar, with a smoked black one and a white one lying beside it" width="100%"></p>

[![CI](https://github.com/zakijss/MiniRadar/actions/workflows/ci.yml/badge.svg)](https://github.com/zakijss/MiniRadar/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/zakijss/MiniRadar)](https://github.com/zakijss/MiniRadar/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

# MiniRadar

The devices on your Wi-Fi network, and Wake-on-LAN, from a Miyoo Mini Plus. Part of [MiniDeck](https://github.com/zakijss/MiniDeck).

- Each device's IP and MAC address, maker, name and common open ports, from ordinary connections only.
- Scans up to 254 addresses, a few hundred probes a second at most, so the Wi-Fi isn't flooded.
- A list of computers to wake with Wake-on-LAN, added from a scan or by MAC address, each showing when a scan last saw it.

<p align="center"><img src="docs/desk.jpg" alt="A grey Miyoo Mini Plus on a stand on a sunlit window sill, listing network devices in MiniRadar, with a smoked black one beside it open at one device" width="100%"></p>

## Install

Open [MiniMart](https://github.com/zakijss/MiniMart) and pick **Apps > MiniRadar**: it checks the download and keeps your settings on updates. Or unzip `miniradar-VERSION.zip` from [Releases](https://github.com/zakijss/MiniRadar/releases/latest) onto the root of the SD card and restart. Every MiniDeck app comes in one zip too: [minideck-all.zip](https://github.com/zakijss/MiniDeck/releases/latest/download/minideck-all.zip).

Needs a Miyoo Mini Plus on Onion OS 4.4, with Wi-Fi. Other devices and firmwares aren't supported.

## Using it

MiniRadar scans the network the Miyoo is on when it opens. **X** scans again, **A** opens a device, **Y** saves it for Wake, **L1**/**R1** switch between Devices and Wake, and **L2**/**R2** jump to the top and bottom of a list. On the Wake tab, **A** wakes a device and **Y** edits it. **START** opens Settings: sort the devices by address, name or maker, or turn the port check off for quicker scans.

A saved device is listed under its saved name, and shows Awake on the Wake tab when the last scan found it. A device no scan found before is marked New until you next open the app.

The wake list is kept in `App/MiniRadar/devices.json`, Settings in `settings.json` beside it, and the devices earlier scans found (MAC addresses only, at most 512) in `seen.json`.

## Building

```sh
make check                     # vet, tests, scripts
make package VERSION=1.0.0     # dist/: the release zip, app.json, SHA256SUMS
make deploy HOST=root@miyoo    # build and copy to a device over SSH
```

Go runs in Docker unless you pass `GO=go` (Go 1.27). The interface, themes and networking come from the [MiniDeck library](https://github.com/zakijss/MiniDeck); see its [development notes](https://github.com/zakijss/MiniDeck/blob/main/docs/DEVELOPMENT.md) and [contributing guide](https://github.com/zakijss/MiniDeck/blob/main/CONTRIBUTING.md).

## License

[MIT](LICENSE). Third-party components are listed in [NOTICE](NOTICE). Not made by or affiliated with Miyoo.
