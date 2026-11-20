/*
Package rootcmd assembles the packetflix service, so that downstream modules can
build their own packetflix variants from the same parts as the stock binary,
without forking packetflix.

The stock main package is just:

	func main() {
		if err := rootcmd.Execute(rootcmd.New(rootcmd.Variant{Version: version})); err != nil {
			os.Exit(1)
		}
	}

[Execute] takes care of the process-level details, such as locking the main
goroutine to its OS thread and stopping on SIGINT and SIGTERM.

# Assembling a Variant

A downstream variant sets its own name and version in a [Variant], and returns
its own [http.Server]s from [Variant.Servers]. Each server has its own address,
handler, optional TLS configuration, and server settings such as timeouts. All
servers start together and shut down together.

The main case is a variant behind a reverse proxy that terminates TLS, such as
a device's nginx: two plain-HTTP listeners, each reached only through its own
proxy route. Here, the UI listener serves the UI, the discovery proxy, and a
ticket endpoint behind a route requiring a login, and the capture listener
serves only “/capture” behind a route without login, admitting only requests
presenting a ticket:

	v := rootcmd.Variant{
		Name:    "variantflix",
		Version: version,
		Servers: func(cmd *cobra.Command) ([]*http.Server, error) {
			gw := discovery.Address(cmd)

			ui := http.NewServeMux()
			ui.Handle("/version", rootcmd.NewVersionHandler(cmd))
			ui.Handle("/ticket", newTicketHandler())
			ui.Handle("/capture", http.NotFoundHandler())
			ui.Handle("/", packetflix.NewDiscoveryProxy(gw))

			capture := http.NewServeMux()
			capture.Handle("/capture", packetflix.NewCaptureHandlerWith(gw,
				packetflix.WithAdmission(admitTicket),
				packetflix.WithObserver(audit),
				packetflix.WithPingInterval(wsping.Interval(cmd))))

			return []*http.Server{
				{Addr: ":5001", Handler: rootcmd.LogRequests(cmd, ui)},
				{Addr: ":5002", Handler: rootcmd.LogRequests(cmd, capture), ReadHeaderTimeout: 10 * time.Second},
			}, nil
		},
	}

The websocket pings keep a capture connection alive while the capture produces
no packet data: reverse proxies close upgraded connections when the proxied
server has sent nothing for some time, such as nginx after its
“proxy_read_timeout”. Choose an interval well below the proxy's timeout. The
stock packetflix service pings every 5 minutes by default, configurable using
the “--wsping-interval” CLI flag, which [wsping.Interval] reads; downstream
variants switch pings on for their own capture handlers using
[packetflix.WithPingInterval].

Behind a reverse proxy, the peer address of every request is the proxy's, and
requests arrive without TLS. Nothing in packetflix decides anything based on
network addresses or TLS; the stock request logging only logs the peer address.
The routes of the reverse proxy may strip a path prefix, as packetflix doesn't
build URLs or redirects from requests itself. However, an [http.ServeMux]
redirects requests with unclean paths, such as “//capture”, to their cleaned
paths, which then lack the stripped prefix.

As a fallback, for instance where the reverse proxy cannot provide a route for
the capture listener, the capture listener can serve TLS itself by setting its
server's TLSConfig:

	{
		Addr:              ":5443",
		Handler:           rootcmd.LogRequests(cmd, capture),
		TLSConfig:         &tls.Config{GetCertificate: deviceCertificate},
		ReadHeaderTimeout: 10 * time.Second,
	}

# Parts

  - [NewMux] for the stock endpoints, or [NewVersionHandler],
    [packetflix.NewCaptureHandler], [packetflix.NewCaptureHandlerWith], and
    [packetflix.NewDiscoveryProxy] for composing other demultiplexers. Leave out
    an endpoint by not registering it. Note that the discovery proxy at “/”
    catches all requests not registered otherwise, including “/capture”.
  - [LogRequests] for the “--log-requests” and “--log-headers” request
    logging, and any other middleware wrapping an [http.Handler].
  - [discovery.Address] and [discovery.IsProxying] for the discovery service
    flags, and [wsping.Interval] for the websocket ping interval flag.
  - the [packetflix.CaptureOption]s: [packetflix.WithAdmission] for deciding
    about capture requests before the websocket upgrade, optionally supplying
    the capture arguments; [packetflix.WithObserver] for learning about the
    capture lifecycle and stopping captures from the server side; and
    [packetflix.WithPingInterval] for keeping capture connections alive.

Wrap the Rewrite function of the [httputil.ReverseProxy] returned by
[packetflix.NewDiscoveryProxy] to add request headers. For instance, when the
reverse proxy strips a path prefix from UI requests without telling, set
“X-Forwarded-Prefix” to the configured prefix, so that the Ghostwire UI loads
its assets from the correct URLs:

	proxy := packetflix.NewDiscoveryProxy(gw)
	rewrite := proxy.Rewrite
	proxy.Rewrite = func(pr *httputil.ProxyRequest) {
		rewrite(pr)
		pr.Out.Header.Set("X-Forwarded-Prefix", uiPrefix)
	}

# CLI Flags

[New] adds the CLI flags of all clippy [cliplugin.SetupCLI] plugins registered
at that time; [cliplugin.BeforeCommand] plugins run before the service starts.
Importing this package registers the stock flags of the packetflix cmd/cli
packages, as well as the clippy debug flags. A downstream adds its own flags by
registering its own plugins from the init function of a package its main
package imports:

	func init() {
		plugger.Group[cliplugin.SetupCLI]().Register(
			func(cmd *cobra.Command) {
				cmd.PersistentFlags().Duration("max-capture-duration", time.Hour, "maximum duration of a capture")
			}, plugger.WithPlugin("variantflix"))
	}

[Variant.Servers] then reads the flags from the passed command, as usual.

[discovery.Address]: https://pkg.go.dev/github.com/siemens/packetflix/cmd/cli/discovery#Address
[discovery.IsProxying]: https://pkg.go.dev/github.com/siemens/packetflix/cmd/cli/discovery#IsProxying
[wsping.Interval]: https://pkg.go.dev/github.com/siemens/packetflix/cmd/cli/wsping#Interval
[cliplugin.SetupCLI]: https://pkg.go.dev/github.com/thediveo/clippy/cliplugin#SetupCLI
[cliplugin.BeforeCommand]: https://pkg.go.dev/github.com/thediveo/clippy/cliplugin#BeforeCommand
*/
package rootcmd
