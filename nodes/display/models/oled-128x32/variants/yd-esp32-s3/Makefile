PYTHON ?= uv run python
PIO ?= uv run pio
CONFIG ?= config.local.yaml
PORT ?= /dev/cu.usbmodemXXXX
BAUD ?= 115200

.PHONY: proto test native build size upload monitor check

proto:
	$(PYTHON) scripts/generate_proto.py

test:
	$(PYTHON) -m pytest -q

native: proto
	$(PIO) test -e native

build: proto
	env APP_CONFIG_FILE="$(CONFIG)" $(PIO) run -e yd_esp32s3

size: proto
	env APP_CONFIG_FILE="$(CONFIG)" $(PIO) run -e yd_esp32s3 -t size

upload: proto
	env APP_CONFIG_FILE="$(CONFIG)" $(PIO) run -e yd_esp32s3 -t upload --upload-port "$(PORT)"

monitor:
	$(PIO) device monitor -e yd_esp32s3 -p "$(PORT)" -b "$(BAUD)" --dtr 0 --rts 0

check: test native
