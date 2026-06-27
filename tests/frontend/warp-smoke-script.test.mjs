import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';

const root = process.cwd();
const script = path.join(root, 'scripts', 'smoke_warp_clearance.sh');

function resolveShell() {
  if (process.platform !== 'win32') {
    return 'sh';
  }
  const result = spawnSync('where.exe', ['sh'], { encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);
  return result.stdout.split(/\r?\n/).find(Boolean);
}

function shellPathName(file) {
  if (process.platform !== 'win32') {
    return file;
  }
  return file
    .replaceAll(path.sep, '/')
    .replace(/^([A-Za-z]):/, (_, drive) => `/${drive.toLowerCase()}`);
}

function writeExecutable(dir, name, body) {
  const file = path.join(dir, name);
  fs.writeFileSync(file, body, { mode: 0o755 });
  fs.chmodSync(file, 0o755);
}

test('warp smoke script redacts proxy credentials and prints HTTP diagnostics', () => {
  const binDir = fs.mkdtempSync(path.join(os.tmpdir(), 'gork-warp-smoke-'));
  try {
    writeExecutable(
      binDir,
      'docker',
      `#!/usr/bin/env sh
if [ "$1" = "compose" ] && [ "$4" = "exec" ]; then
  exit 0
fi
echo "unexpected docker invocation: $*" >&2
exit 1
`,
    );
    writeExecutable(
      binDir,
      'curl',
      `#!/usr/bin/env sh
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    shift
    out="$1"
  fi
  shift || true
done
if [ -n "$out" ]; then
  printf 'HTTP/1.1 503 Service Unavailable\\r\\nServer: stub-proxy\\r\\n\\r\\n' > "$out"
fi
printf '503'
exit 0
`,
    );

    const shell = resolveShell();
    const result = spawnSync(shell, [shellPathName(script)], {
      cwd: root,
      env: {
        ...process.env,
        CURL_BIN: shellPathName(path.join(binDir, 'curl')),
        DOCKER_BIN: shellPathName(path.join(binDir, 'docker')),
        PROXY_URL: 'http://user:secret@127.0.0.1:40080',
        TARGET_URL: 'https://example.invalid',
      },
      encoding: 'utf8',
    });

    assert.equal(result.status, 1);
    assert.match(result.stdout, /http:\/\/\*\*\*@127\.0\.0\.1:40080/);
    assert.doesNotMatch(result.stdout, /secret/);
    assert.match(result.stderr, /proxy smoke failed: curl_exit=0 status=503/);
    assert.match(result.stderr, /Server: stub-proxy/);
  } finally {
    fs.rmSync(binDir, { recursive: true, force: true });
  }
});
