/*
Package requestlog provides the “--log-requests” and “--log-headers” CLI flags
for logging the HTTP and websocket requests to the capture service.

The flags are automatically added to the root command as persistent flags. The
command then checks whether to log requests by calling [IsLogging]:

	if requestlog.IsLogging(cmd) {
		// ...
	}
*/
package requestlog
