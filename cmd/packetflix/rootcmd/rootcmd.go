// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package rootcmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/thediveo/clippy"
	_ "github.com/thediveo/clippy/debug"
	"golang.org/x/sync/errgroup"

	"github.com/siemens/packetflix/cmd/cli/service"
)

// Variant describes a packetflix service variant, such as the stock packetflix
// service or a downstream customization. The zero Variant is the stock
// packetflix service.
type Variant struct {
	// Name of the command, also reported by the “/version” endpoint; defaults
	// to “packetflix”.
	Name string
	// Version reported by “--version” and the “/version” endpoint, without
	// any leading “v”; defaults to the version of the packetflix module.
	Version string
	// Servers returns the HTTP servers to run after the CLI flags have been
	// parsed; defaults to the single stock server on the “--port” service port,
	// serving the [NewMux] endpoints with [LogRequests].
	//
	// Each server listens on its Addr, which must be a “host:port” TCP
	// address, and serves TLS if its TLSConfig is set, using the TLSConfig's
	// certificates or certificate callbacks. All other server settings, such
	// as timeouts, apply as usual. Please note that websocket upgrades require
	// HTTP/1.1. The servers start together and shut down together when the
	// command's context is done or when any one of them fails.
	Servers func(cmd *cobra.Command) ([]*http.Server, error)
}

// New returns a new Cobra "root" command for the passed service variant. Its
// CLI flags are added by the clippy [cliplugin.SetupCLI] plugins registered at
// this time, including those of the packetflix cmd/cli packages. Run the
// command using [Execute].
//
// [cliplugin.SetupCLI]: https://pkg.go.dev/github.com/thediveo/clippy/cliplugin#SetupCLI
func New(v Variant) (rootCmd *cobra.Command) {
	servers := v.Servers
	if servers == nil {
		servers = stockServers
	}
	rootCmd = &cobra.Command{
		Use:     cmp.Or(v.Name, "packetflix"),
		Short:   "Live network packet capture streaming from untouchable containers",
		Version: buildVersion(v.Version),
		Args:    cobra.NoArgs,
		// See: https://github.com/spf13/cobra/issues/340
		SilenceUsage: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return clippy.BeforeCommand(rootCmd)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return serve(cmd, servers)
		},
	}
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	clippy.AddFlags(rootCmd) // ...runs all registered SetupCLI plugins.
	return
}

// Execute runs the passed root command, as returned by [New], until it
// terminates or the process receives a SIGINT or SIGTERM. Execute must be
// called from the main goroutine.
//
// Cobra already reports any error returned, see also:
// https://github.com/spf13/cobra/issues/304
func Execute(rootCmd *cobra.Command) error {
	// Not strictly necessary, but makes life easier for lxkns/Ghostwire:
	// without it, we might end up with the situation where the OS-level thread
	// from which we started the dumpcap binary and which we thus switched into
	// the target's network namespace ends up being the main thread and thus the
	// process itself. Now that then makes lxkns see packetflix' process being
	// the ealdorman in a network namespace different from its container's
	// network namespace.
	runtime.LockOSThread()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return rootCmd.ExecuteContext(ctx)
}

// stockServers returns the single stock packetflix server.
func stockServers(cmd *cobra.Command) ([]*http.Server, error) {
	return []*http.Server{{
		Addr:    fmt.Sprintf(":%d", service.GetPort(cmd)),
		Handler: LogRequests(cmd, NewMux(cmd)),
	}}, nil
}

// serve runs the capture service servers until the command's context is done
// or any server fails.
func serve(cmd *cobra.Command, newServers func(*cobra.Command) ([]*http.Server, error)) error {
	slog.Info("Packetflix Capture-as-a-Service", slog.String("version", cmd.Root().Version))
	servers, listeners, err := listen(cmd, newServers)
	if err != nil {
		return fmt.Errorf("cannot start capture service: %w", err)
	}
	// ServeTLS doesn't close its listener when it fails before serving.
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	// The first server failing cancels ctx, so all servers then shut down.
	group, ctx := errgroup.WithContext(cmd.Context())
	for idx, server := range servers {
		listener := listeners[idx]
		slog.Info("starting capture service websocket server",
			slog.String("addr", listener.Addr().String()))
		group.Go(func() error {
			var err error
			if server.TLSConfig != nil {
				err = server.ServeTLS(listener, "", "")
			} else {
				err = server.Serve(listener)
			}
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		})
	}
	group.Go(func() error {
		<-ctx.Done()
		// Shutdown neither closes nor waits for hijacked websocket connections.
		var err error
		for _, server := range servers {
			if shutdownErr := server.Shutdown(context.Background()); err == nil {
				err = shutdownErr
			}
		}
		return err
	})
	err = group.Wait()
	if err != nil {
		return err
	}
	slog.Info("capture service stopped")
	return nil
}

// listen returns the servers to run together with their listeners.
func listen(cmd *cobra.Command, newServers func(*cobra.Command) ([]*http.Server, error)) ([]*http.Server, []net.Listener, error) {
	servers, err := newServers(cmd)
	if err != nil {
		return nil, nil, err
	}
	if len(servers) == 0 {
		return nil, nil, errors.New("no servers")
	}
	listeners := make([]net.Listener, 0, len(servers))
	for _, server := range servers {
		listener, err := net.Listen("tcp", server.Addr)
		if err != nil {
			for _, listener := range listeners {
				_ = listener.Close()
			}
			return nil, nil, err
		}
		listeners = append(listeners, listener)
	}
	return servers, listeners, nil
}
