# DEEP 3.1.0 release and upgrade

DEEP 3.1.0 is the reference implementation version. The interoperable
wire format remains version 2 with ALPN deep/2; configuration files keep
version: 2. The application profile remains app/1 and the native proxy remains
Proxy/1. Existing 2.x identities, server pins, direct adapters, and configuration
remain usable. Do not regenerate keys or change a JSON version to 3.

## What changed

DEEP 3.1.0 registers `deep://` links on Linux and Windows and opens them in an
isolated Firefox or Chromium-based window. It uses the system-default browser
when supported. The browser bridge uses the configured DEEP client and verifies
page, asset, and form responses. A connected HellNode session can supply the
proxy for desktop `.hell` links. See the [browser integration guide](BROWSER-INTEGRATION.md).

The 3.0.1 local website routing fixes for Caddy and other servers carry forward,
including loopback HTTP(S) upstreams and same-origin header handling. The
backend and optional legacy Windows viewer share version 3.1.0.

The 3.0.0 fixes remain: no silent replay of submitted actions, validated resource
components, aligned viewer URI validation, and graceful SIGTERM shutdown.

Proxy configuration remains optional with no provider, address, or port
selected by default. The viewer remains disabled until explicitly enabled.
The core does not depend on HellNet or HTTP; HTTP is only an optional local
application provider selected by the operator.

## Install an update

Close DEEP servers/viewers and applications that own DEEP child processes.
Back up existing executable files. Replace the backend, and replace the complete
bin/viewer folder if you use the bundled viewer. Keep config.json, private keys,
certificates, content folders, and per-user preferences.

For HellNet, install **HellNet 3.3.1 or newer** as well. This compatibility update
accepts DEEP 3.x and recognizes its server startup message. Earlier HellNet
versions reject DEEP 3.x even though the wire format remains compatible.
Your .hell service identities and the local introduction address stay the same.
Local website hosting with a port or URL requires DEEP 3.0.1+ on the hosting
computer; HellNet 3.3.2 adds the URL input. Existing DEEP 3.x visitors remain
wire-compatible.

Verify the backend with:

~~~powershell
.\bin\deep.exe version
.\bin\deep.exe demo
.\bin\deep.exe proxy status
.\bin\deep.exe viewer status
~~~

Only enable/configure the optional proxy and viewer if you intend to use them.

## Local release archives

The release script builds CLI archives for Windows, Linux, and macOS on amd64
and arm64, plus a source archive. With a matching self-contained win-x64 viewer
build it also creates a Windows desktop ZIP:

~~~powershell
.\scripts\build.ps1 -WithViewer
python .\scripts\release.py --windows-viewer .\bin\viewer --output .\dist\v3.1.0
~~~

The output directory must not exist. BUILD-MANIFEST.json records target,
toolchain, protocol profiles and archive hashes. SHA256SUMS covers the archives
and manifest. The desktop ZIP contains the same backend plus the optional
viewer and its redistributed licenses. No settings, identities, or browser
profiles are included.

These are local artifacts; this script does not publish, upload, tag a repository,
or sign releases. Checksums check file integrity but do not prove publisher
identity. Windows amd64 is exercised locally; cross-compilation for other
platforms is not an execution test. See [validation](VALIDATION-V3.md) for evidence.
