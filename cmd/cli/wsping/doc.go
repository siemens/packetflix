/*
Package wsping provides the “--wsping-interval” CLI flag for specifying the
interval of the websocket pings sent on capture connections. Pings keep capture
connections alive through reverse proxies that close upgraded connections after
some time without any data from the capture service, while a capture produces
no packet data.

The flag is automatically added to the root command as a persistent flag. The
command then gets the interval by calling [Interval]:

	interval := wsping.Interval(cmd)
*/
package wsping
