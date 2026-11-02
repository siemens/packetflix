// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

// Parses and checks the capture parameters transmitted in the request to
// establish a live websock capture stream. It then returns a container data
// structure with the necessary details to join the correct Linux kernel network
// namespace and the list of network interfaces to capture from.

package packetflix

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/siemens/csharg/v2/api"
	"github.com/thediveo/lxkns/model"
)

// Args represent the capture arguments.
type Args struct {
	// Details of the capture target.
	*api.Target
	// Optional packet filter expression.
	CaptureFilter string
	// No promiscuous mode, please!
	KeepChaste bool
}

// parseCaptureParams parses the capture parameters from the HTTP request,
// refreshing stale target information from the discovery service at the
// specified "host:port" address.
func parseCaptureParams(req *http.Request, conn *WSConn, discovery string) (*Args, error) {
	params := captureQuery(req)
	target, err := targetFromParams(params)
	if err != nil {
		return nil, err
	}
	args := &Args{Target: target}

	// If we got unlucky, then we now need to fetch up-to-date information from
	// the discovery service: we need a correct network namespace identifier.
	if !isCurrent(args.Target) {
		args.NetNS = 0
	}
	if args.NetNS == 0 {
		_, nifsRequested := params["nif"]
		if err := refreshTarget(args, conn, discovery, nifsRequested); err != nil {
			return nil, err
		}
	}

	// Now work on the separate nif URL query parameter to update/replace the
	// list of network interfaces in the container description with only those
	// network interfaces we will capture from. If nif is not specified, then
	// automatically all network interfaces as listed will apply -- if we know
	// the list; otherwise we'll fall back to "any", which has the drawback of
	// using a "cooked" packet stream coming from a single virtual nif instead
	// of differentiating the real nifs.
	if nifs, ok := params["nif"]; ok && nifs[0] != "any" {
		args.NetworkInterfaces = strings.Split(nifs[0], "/")
	}
	if len(args.NetworkInterfaces) == 0 || args.NetworkInterfaces[0] == "" {
		// Last resort fallback if we don't know the exact list of network
		// interfaces. This will remove the capture of the detail information
		// from which specific network interface a packet comes from.
		args.NetworkInterfaces = []string{"any"}
	}

	if f, ok := params["filter"]; ok {
		args.CaptureFilter = f[0]
	}
	_, args.KeepChaste = params["chaste"]
	return args, nil
}

// captureQuery returns the URL query parameters of the request, overridden by
// any Clustershark-specific HTTP headers.
//
// "Würgs" (works) around a bug in the Kubernetes remote API pod proxy verb: this
// bug drops the URL query parameters from a websocket connect. So to get our job
// done, we resort to use application-specific (service-specific) HTTP headers,
// which we then here map back onto their original URL query parameters. In
// consequence, the service-specific HTTP headers always take precedence.
//
// Nota bene: the Go http package insists of canonical header keys, see:
// https://godoc.org/net/http#CanonicalHeaderKey. This means that the first
// letter as well as any letters immediately following a hyphen will be upper
// case.
func captureQuery(req *http.Request) url.Values {
	params := req.URL.Query()
	for header, param := range map[string]string{
		"Clustershark-Container": "container",
		"Clustershark-Nif":       "nif",
		"Clustershark-Filter":    "filter",
		"Clustershark-Chaste":    "chaste",
	} {
		if values, ok := req.Header[header]; ok {
			params[param] = values
		}
	}
	return params
}

// targetFromParams returns the capture target described by either the
// "container" or the "netns" query parameter, which are mutually exclusive.
func targetFromParams(params url.Values) (*api.Target, error) {
	cp, cok := params["container"]
	netnsp, netnsok := params["netns"]
	switch {
	case cok && netnsok:
		return nil, fmt.Errorf("container and netns query parameters are mutually exclusive")
	case cok:
		target := new(api.Target)
		if err := json.Unmarshal([]byte(cp[0]), target); err != nil {
			return nil, fmt.Errorf("invalid container/target description: %s", err.Error())
		}
		return target, nil
	case netnsok:
		netns, err := strconv.ParseInt(netnsp[0], 10, strconv.IntSize)
		if err != nil || netns <= 0 {
			return nil, fmt.Errorf("invalid netns \"%s\"", netnsp[0])
		}
		return &api.Target{NetNS: int(netns)}, nil
	}
	return nil, fmt.Errorf("either container or netns query parameter required")
}

// isCurrent returns false if the target's network namespace identifier might
// have turned stale, because the target's (usually "root") process is gone or
// has a different start time. Without PID and start time, the target is taken
// as current.
func isCurrent(target *api.Target) bool {
	if target.Pid <= 0 || target.StartTime <= 0 {
		return true
	}
	proc := model.NewProcess(model.PIDType(target.Pid), false)
	return proc != nil && proc.Starttime == uint64(target.StartTime)
}

// refreshTarget updates the target details with the up-to-date information
// from the discovery service at the specified "host:port" address, if the
// discovery service knows the target by its name and prefix. Unless nif=
// network interfaces have been requested, any original network interfaces of
// the target are kept.
func refreshTarget(args *Args, conn *WSConn, discovery string, nifsRequested bool) error {
	conn.log.Debug("updating container meta data from local discovery service", slog.String("discovery", discovery))
	targets, err := discoverTargets(discovery)
	if err != nil {
		return err
	}
	idx := slices.IndexFunc(targets, func(t *api.Target) bool {
		return t.Name == args.Name && t.Prefix == args.Prefix
	})
	if idx < 0 {
		return nil
	}
	target := targets[idx]
	name := target.Name
	if target.Prefix != "" {
		name = target.Prefix + ":" + name
	}
	conn.log.Debug("updating target information", slog.String("type", target.Type), slog.String("name", name))
	orignifs := args.NetworkInterfaces
	args.Target = target
	if !nifsRequested && len(orignifs) > 0 {
		args.NetworkInterfaces = orignifs
	}
	return nil
}

// discoverTargets returns the capture targets known to the discovery service
// at the specified "host:port" address.
func discoverTargets(discovery string) (api.Targets, error) {
	httpc := &http.Client{Timeout: DiscoveryDeadline}
	resp, err := httpc.Get(fmt.Sprintf("http://%s/mobyshark", discovery))
	if err != nil {
		return nil, fmt.Errorf("cannot update container meta data: %s", err.Error())
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cannot update container meta data: %s", err.Error())
	}
	var targets api.GwTargetList
	if err := json.Unmarshal(body, &targets); err != nil {
		return nil, fmt.Errorf("cannot update container/target meta data: %s", err.Error())
	}
	return targets.Targets, nil
}
