/*
Package service provides the “--port” (“-p”) CLI flag for specifying the TCP
port to expose the capture service on.

The flag is automatically added to the root command as a persistent flag. The
command then gets the port by calling [GetPort]:

	port := service.GetPort(cmd)
*/
package service
