// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

// Parses and checks the capture parameters transmitted in the request to
// establish a live websock capture stream. It then returns a container data
// structure with the necessary details to join the correct Linux kernel network
// namespace and the list of network interfaces to capture from.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/siemens/csharg/api"
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

// parseCaptureParams parses the capture parameters from the HTTP request.
func parseCaptureParams(req *http.Request, conn *WSConn) (
	args *Args, err error) {
	params := req.URL.Query()
	// "Würgs" (works) around a bug in the Kubernetes remote API pod proxy verb:
	// this bug drops the URL query parameters from a websocket connect. So to
	// get our job done, we resort to use application-specific
	// (service-specific) HTTP headers, which we then here map back onto their
	// original URL query parameters. In consequence, the service-specific HTTP
	// headers always take precedence.
	//
	// Nota bene: the Go http package insists of canonical header keys, see:
	// https://godoc.org/net/http#CanonicalHeaderKey. This means that the first
	// letter as well as any letters immediately following a hyphen will be
	// upper case.
	if cntr, ok := req.Header["Clustershark-Container"]; ok {
		params["container"] = cntr
	}
	if nifs, ok := req.Header["Clustershark-Nif"]; ok {
		params["nif"] = nifs
	}
	if filter, ok := req.Header["Clustershark-Filter"]; ok {
		params["filter"] = filter
	}
	if chaste, ok := req.Header["Clustershark-Chaste"]; ok {
		params["chaste"] = chaste
	}

	// Please note that the "container" and "netns" URL query parameters are
	// mutually exclusive: there can be only one of them present.
	cp, cok := params["container"]
	netnsp, netnsok := params["netns"]
	if cok && netnsok {
		return nil, fmt.Errorf("container and netns query parameters are mutually exclusive")
	}
	if cok {
		// If a full-blown container meta data query parameter is present, then use
		// this one...
		args = new(Args)
		args.Target = new(api.Target)
		if err := json.Unmarshal([]byte(cp[0]), args.Target); err != nil {
			return nil, fmt.Errorf("invalid container/target description: %s", err.Error())
		}
	} else if netnsok {
		// ...else create a simple container meta data set from the few things
		// we were only given.
		if netns, err := strconv.ParseInt(netnsp[0], 10, 64); err == nil && netns > 0 {
			args = new(Args)
			args.Target = new(api.Target)
			args.Target.NetNS = int(netns)
		} else {
			return nil, fmt.Errorf("invalid netns \"%s\"", netnsp[0])
		}
	} else {
		return nil, fmt.Errorf("either container or netns query parameter required")
	}

	// With all (or only some) information gathered into the container meta data
	// description, now check whether the data is still current or has turned
	// stale. This can be verified on the basis of the given PID (usually the
	// "root" process inside a container) and its corresponding start time.
	if args.Target.Pid > 0 && args.Target.StartTime > 0 {
		stf, err := os.Open(fmt.Sprintf("/proc/%d/stat", args.Target.Pid))
		if err != nil {
			// We cannot verify the PID and start time, so we assume it to be
			// stale in order to trigger a meta data refresh below.
			args.Target.NetNS = 0
		} else {
			// Just read the first (and only) line from the process stats and break
			// it down into its fields...
			line, _ := bufio.NewReader(stf).ReadString('\n')
			// /proc/PID/stat has the process name in the second field, and the
			// name is enclosed in brackets because it may contain spaces, so we
			// look for the final ')' and only afterwards start splitting the
			// remaining line contents into fields.
			stats := strings.Fields(string(line)[strings.Index(string(line), ")")+2:])
			// starttime is field #22 (counting from #1), so we need to take
			// into account that Go slice indices start at zero.
			if starttime, err := strconv.ParseInt(stats[22-3], 10, 64); err == nil {
				if int64(starttime) != args.Target.StartTime {
					args.Target.NetNS = 0
				}
			} else {
				// In case of issues verifying the start time, play safe here.
				args.Target.NetNS = 0
			}
		}
	}

	// If we got unlucky, then we now need to fetch up-to-date information from
	// the discovery service: we need a correct network namespace identifier.
	if args.Target.NetNS == 0 {
		conn.Debugf("updating container meta data from local discovery service (at port %d)", DiscoveryPort)
		httpc := &http.Client{Timeout: DiscoveryDeadline}
		resp, err := httpc.Get(fmt.Sprintf("http://%s:%d/mobyshark", DiscoveryService, DiscoveryPort))
		if err != nil {
			return nil, fmt.Errorf("cannot update container meta data: %s", err.Error())
		}
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("cannot update container meta data: %s", err.Error())
		}
		var targets api.GwTargetList
		if err := json.Unmarshal(body, &targets); err != nil {
			return nil, fmt.Errorf("cannot update container/target meta data: %s", err.Error())
		}
		for _, t := range targets.Targets {
			if t.Name == args.Target.Name && t.Prefix == args.Target.Prefix {
				// We've found the desired container, so we can now update its
				// meta data.
				name := t.Name
				if len(t.Prefix) > 0 {
					name = fmt.Sprintf("%s:%s", t.Prefix, name)
				}
				conn.Debugf("updating information about %s %s", t.Type, name)
				orignifs := args.Target.NetworkInterfaces
				args.Target = t
				// If there have been a network interface list before the update
				// and there is no nif= URL query parameter specified, then
				// restore the original list of network interfaces to capture
				// from. Otherwise, go with the new list, which might be getting
				// overwritten below in case there is an explicit nif= query
				// parameter.
				if _, ok := params["nif"]; !ok && len(orignifs) > 0 {
					args.Target.NetworkInterfaces = orignifs
				}
				break
			}
		}
	}

	// Now work on the separate nif URL query parameter to update/replace the
	// list of network interfaces in the container description with only those
	// network interfaces we will capture from. If nif is not specified, then
	// automatically all network interfaces as listed will apply -- if we know
	// the list; otherwise we'll fall back to "any", which has the drawback of
	// using a "cooked" packet stream coming from a single virtual nif instead
	// of differentiating the real nifs.
	if nifs, ok := params["nif"]; ok {
		if nifs[0] != "any" {
			args.Target.NetworkInterfaces = strings.Split(nifs[0], "/")
		}
	}
	if len(args.Target.NetworkInterfaces) == 0 || args.Target.NetworkInterfaces[0] == "" {
		// Last resort fallback if we don't know the exact list of network
		// interfaces. This will remove the capture of the detail information
		// from which specific network interface a packet comes from.
		args.Target.NetworkInterfaces = []string{"any"}
	}

	// Get an optional filter expression...
	if f, ok := params["filter"]; ok {
		args.CaptureFilter = f[0]
	}

	// And finally for avoiding getting too promiscuous (mode).
	if _, ok := params["chaste"]; ok {
		args.KeepChaste = true
	}

	return
}
