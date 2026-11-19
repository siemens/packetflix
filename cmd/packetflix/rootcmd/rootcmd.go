// (c) Siemens AG 2026
//
// SPDX-License-Identifier: MIT

package rootcmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/spf13/cobra"
	"github.com/thediveo/clippy"
	_ "github.com/thediveo/clippy/debug"

	"github.com/siemens/packetflix/cmd/cli/discovery"
	"github.com/siemens/packetflix/cmd/cli/requestlog"
	"github.com/siemens/packetflix/cmd/cli/service"
)

// New returns the Cobra "root" command, thus the packetflix service itself,
// reporting the passed version, if any, or otherwise the version of the
// packetflix module.
func New(version string) (rootCmd *cobra.Command) {
	rootCmd = &cobra.Command{
		Use:     "packetflix",
		Short:   "Live network packet capture streaming from untouchable containers",
		Version: buildVersion(version),
		Args:    cobra.NoArgs,
		// See: https://github.com/spf13/cobra/issues/340
		SilenceUsage: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return clippy.BeforeCommand(rootCmd)
		},
		RunE: serve,
	}
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	clippy.AddFlags(rootCmd) // ...runs all registered SetupCLI plugins.
	return
}

// serve runs the capture service until the command's context is done.
func serve(cmd *cobra.Command, _ []string) error {
	slog.Info("Packetflix Capture-as-a-Service", slog.String("version", cmd.Root().Version))
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", service.GetPort(cmd)))
	if err != nil {
		return fmt.Errorf("cannot start capture service: %w", err)
	}
	server := &http.Server{
		Handler: requestLogger(
			newDemux(discovery.Address(cmd), discovery.IsProxying(cmd), cmd.Root().Version),
			requestlog.IsLogging(cmd)),
	}
	slog.Info("starting capture service websocket server",
		slog.String("addr", listener.Addr().String()))
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	select {
	case err := <-served:
		return err
	case <-cmd.Context().Done():
	}
	// Shutdown neither closes nor waits for hijacked websocket connections.
	if err := server.Shutdown(context.Background()); err != nil {
		return err
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	slog.Info("capture service stopped")
	return nil
}
