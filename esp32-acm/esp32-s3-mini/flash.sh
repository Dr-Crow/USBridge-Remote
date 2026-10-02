#!/bin/sh
# Builds and flashes a dongle that is already running this firmware: asks it
# over the USB maintenance channel to reboot into the ROM download mode
# (works whether or not the CDC-ACM control channel is up), then uploads.
# A blank or bricked board has to be put there by hand (hold BOOT, plug in).
set -e
cd "$(dirname "$0")"
PIO="${PIO:-$HOME/.platformio/penv/bin/pio}"
"$PIO" run
if ! lsusb -d 303a:1001 >/dev/null 2>&1; then
    ../tools/dongle_usb.py bootloader || true
    i=0
    until lsusb -d 303a:1001 >/dev/null 2>&1; do
        i=$((i + 1))
        [ "$i" -gt 20 ] && { echo "board did not enter download mode" >&2; exit 1; }
        sleep 0.5
    done
    sleep 1
fi
PORT="${PORT:-$(ls /dev/ttyACM* | head -1)}"
"$PIO" run -t upload --upload-port "$PORT"
