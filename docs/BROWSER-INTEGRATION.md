# Add DEEP to a browser

If you ship a browser and want it to open `deep://` links, choose how the
browser hands the navigation to DEEP.

| What you want | Integration |
| --- | --- |
| Let users type or click a `deep://` link from an existing browser | Register DEEP as the desktop link handler. DEEP opens an isolated browser window. |
| Render a `deep://` site in the tab where the user entered it | Add a `deep` scheme loader to your browser and use DEEP as its request backend. |

Both use a trusted DEEP client configuration for network adapters, native
proxies and identities. Addresses have the form `deep://node.network/path`.
For example, `deep://node.alpha/` uses the configured `alpha` network.

## Desktop link handoff

Use DEEP 3.1.0 or later. Put the executable at a stable path and put a trusted
`config.json` beside it. Register once per desktop
user. On Linux, the registration command uses `xdg-mime` from `xdg-utils`.

```sh
deep browser-register
```

On Windows, run this from the DEEP distribution folder.

```powershell
.\scripts\register.ps1 -Executable .\bin\deep.exe
```

Type the complete `deep://` URL into the address bar or click a link. Approve
the browser's prompt to open DEEP if one appears. The desktop association
passes just the URL to `deep open-browser-uri URL`. DEEP opens a new isolated
window in the supported system-default browser on Windows and Linux. If the
browser that started the link differs from the default, the window can open
in a different browser. If the address bar searches for the URI, test a
clickable link or run the command below. [Firefox external link handoff](https://firefox-source-docs.mozilla.org/uriloader/exthandler/index.html)

```sh
deep browser 'deep://node.alpha/' --config /absolute/path/to/client.json
```

If you're distributing a browser with DEEP as a companion, your installer can
offer this same per-user registration. To use the built-in browser bridge with
a separate trusted configuration, launch `deep browser URL --config FILE`
from your browser process. Pass the URL and config path as separate process
arguments. Keep the DEEP process alive for the lifetime of its window.

## Load the scheme inside your browser

An in-tab integration needs a browser-side scheme loader for both document
navigation and subresource requests. A webpage's `registerProtocolHandler()`
is restricted to safelisted schemes and `web+` names, so it cannot register
the literal `deep` scheme. An extension can use native messaging to talk to a
companion process. The extension still needs a browser-provided way to
receive the navigation, and the native host must implement the browser's
message framing. DEEP's `request` command uses a separate stdin/stdout contract.
[HTML custom protocol handlers](https://html.spec.whatwg.org/multipage/system-state.html#custom-handlers) · [Chromium native messaging](https://developer.chrome.com/docs/extensions/develop/concepts/native-messaging)

If you maintain a Chromium-based browser, look at
`ContentBrowserClient::CreateNonNetworkNavigationURLLoaderFactory` and
`RegisterNonNetworkSubresourceURLLoaderFactories` for the navigation and
subresource entry points. In a Firefox fork, start with `nsIProtocolHandler`
and the channel it creates. Check the signatures for the engine version you
ship. These hooks let your browser supply DEEP responses to the renderer.
[Chromium embedder interface](https://chromium.googlesource.com/chromium/src/+/main/content/public/browser/content_browser_client.h) · [Firefox protocol handler interface](https://searchfox.org/firefox-main/source/netwerk/base/nsIProtocolHandler.idl)

1. Route `deep://` navigation and resource requests to a browser-process
   loader. Validate the URL with DEEP's parser. Keep fragments in browser
   history and use the original DEEP URL for relative URL resolution.
2. Send the URI, method, allowed end-to-end headers and body to DEEP. Start
   with the CLI contract below. A Go frontend can instead use
   `LoadClientConfig` and `Client.Exchange`.
3. Commit the renderer response after DEEP reports success. Use its status,
   media type, allowed response headers, and verified body. An application
   response such as 404 can have a successful DEEP process exit.
4. Keep browser security checks in that path. Isolate site origins by
   scheme and full DEEP authority. Apply cookie scoping, origin checks,
   redirects, and permissions in the browser. Keep DEEP credentials,
   configuration selection and proxy controls in trusted browser code.
   Keep DEEP pages at ordinary web-content privilege. For a privacy
   profile, keep subresources and form requests on the configured route
   and deny external network requests by default.

### CLI request contract

The browser launches the configured DEEP executable directly, without a
shell. Give it one URI and a config path selected by the browser or user.
Write one JSON request to stdin and close it. Body is base64, including
the empty string for an empty body. Use a list of header pairs to preserve
duplicate fields. `request` supports uploads up to 1 MiB and responses up to
16 MiB. The example below sets a lower 4 MiB response limit.

```python
import json
import subprocess


def fetch_deep_page(executable, config_path, uri):
    request = {"method": "GET", "headers": [], "body": ""}
    result = subprocess.run(
        [
            executable, "request", uri, "--config", config_path,
            "--info", "--timeout", "15s", "--max-bytes", "4194304",
        ],
        input=json.dumps(request).encode("utf-8"),
        capture_output=True,
        timeout=20,
        check=False,
    )
    if result.returncode != 0:
        raise RuntimeError("DEEP request failed")
    return json.loads(result.stderr), result.stdout
```

Run this example off the UI thread. In the browser loader, cancel the
request when the tab closes or navigates away. If DEEP exits with an error,
discard the output and show a browser-controlled error page. Automatic
retries of writes can duplicate submissions after a timeout or disconnect.
Keep authentication headers and request bodies out of routine diagnostics.
See [the CLI integration contract](INTEGRATION.md#command-line-integration-any-language)
and [the app/1 request and response format](APPLICATIONS.md).

To route through a DEEP network manager, use its trusted client configuration
or have the user run `deep proxy set --address IP:PORT --config FILE` with the
manager's actual loopback listener. That setting selects DEEP's native
[Proxy/1](DEEP-PROXY.md) transport. Keep the manager running during navigation.
For HellNet desktop links, use current versions of both programs. HellNode
enables desktop links when you connect. Registered `deep://…hell/` links use
the active HellNode session from the desktop handler. Use `browser-links off`
in HellNode to disable them for the current session. For a browser with
its own loader, add `--desktop` to the `deep request` arguments to use the
same selection. Session routing only applies to `.hell` links. If no active
session is published, DEEP uses the supplied client config. A published
session with an unavailable endpoint or mismatched pin fails closed. A
missing route or failed identity check should surface as an error in the tab.

## Try it locally

Create a test node in a new directory and leave the server running in
one terminal.

```sh
deep init --authority node.alpha --dir node-alpha --address 127.0.0.1:9761
deep serve --config node-alpha/server.json
```

In another terminal, open it with the supplied private client configuration.
The initialization command generates credentials for that configuration.
Keep the files together so the relative credential paths resolve.

```sh
deep browser 'deep://node.alpha/' --config node-alpha/client.json
```

For an in-tab integration, point your loader at the same client config and
navigate to `deep://node.alpha/`. The default content shows a DEEP greeting.
Add an `index.html` with a same-site stylesheet and verify both requests use
DEEP. Then test separate authorities for origin isolation, a wrong server pin
for rejection, and a stopped proxy for an error with no direct fallback. If
you support application requests, test a form submission and a same-origin
redirect as well.
