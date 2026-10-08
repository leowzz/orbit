#!/usr/bin/env python3
"""Run Android integration tests against a disposable Core, without MQTT.

Requires a running dedicated emulator. This clears ONLY the test application's
local data on that emulator; never point it at a personal device.
"""
import argparse
import hashlib
import os
from pathlib import Path
import subprocess
import tempfile
import time
import urllib.request

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--emulator', default='emulator-5554')
parser.add_argument('--flutter', default='flutter')
args = parser.parse_args()
if not args.emulator.startswith('emulator-'):
    parser.error('use a dedicated Android emulator')
root = Path(__file__).resolve().parent.parent
with tempfile.TemporaryDirectory(prefix='orbit-smoke-') as directory:
    temp = Path(directory)
    for name in ('username', 'password'):
        (temp / name).write_text('test-only')
    devices = ''
    for number in (1, 2):
        token = f'orbit-integration-test-token-{number:08d}'
        digest = hashlib.sha256(token.encode()).hexdigest()
        devices += f'    app-{number}:\n      token_sha256: {digest}\n      platform: android\n'
    (temp / 'core.yaml').write_text('''core:
  id: smoke-core
console:
  listen: 127.0.0.1:17620
  password: test-only-console-password
  database: core.sqlite
mqtt:
  url: mqtt://127.0.0.1:1
  tls:
    enabled: false
  credentials:
    username_file: username
    password_file: password
app:
  listen: 127.0.0.1:17622
  data_dir: data
  devices:
''' + devices)
    binary = str(temp / 'core')
    subprocess.run(['make', 'build-console'], cwd=root, check=True)
    subprocess.run(['go', 'build', '-o', binary, './cmd/orbit-core'], cwd=root, check=True)
    command = [binary, '-config', str(temp / 'core.yaml')]
    subprocess.run(command + ['-seed-inbox'], cwd=root, check=True)
    with (temp / 'core.log').open('w') as log:
        core = subprocess.Popen(command, cwd=root, stdout=log, stderr=log)
        try:
            for attempt in range(100):
                if core.poll() is not None:
                    raise RuntimeError('Core exited; check that port 17622 is free')
                request = urllib.request.Request('http://127.0.0.1:17622/api/v1/status', headers={
                    'Authorization': 'Bearer orbit-integration-test-token-00000001'})
                try:
                    with urllib.request.urlopen(request, timeout=1) as response:
                        assert response.status == 200
                    break
                except OSError:
                    time.sleep(.1)
            else:
                raise RuntimeError('Core did not start')
            # A newly created emulator may not have this package installed yet.
            subprocess.run(['adb', '-s', args.emulator, 'shell', 'pm', 'clear', 'dev.orbit.orbit_android'], check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            subprocess.run([args.flutter, 'test', 'integration_test/foreground_test.dart', '-d', args.emulator], cwd=root / 'nodes/app', env={**os.environ, 'CI':'true'}, check=True)
        finally:
            core.terminate()
            try:
                core.wait(timeout=10)
            except subprocess.TimeoutExpired:
                core.kill()
                core.wait()
