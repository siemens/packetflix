// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package packetflix

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/siemens/csharg/v2/api"
	"github.com/thediveo/spacetest/netns"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/thediveo/success"
)

var _ = Describe("capture parameters", func() {

	var ownnetns int
	var discovery string

	BeforeEach(func() {
		ownnetns = int(netns.CurrentIno())
		discovery = ""
	})

	parse := func(target string, headers ...string) (*Args, error) {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		for idx := 0; idx+1 < len(headers); idx += 2 {
			req.Header[headers[idx]] = []string{headers[idx+1]}
		}
		return parseCaptureParams(req, NewWSConn(), discovery)
	}

	containerParam := func(target api.Target) string {
		return url.QueryEscape(string(Successful(json.Marshal(target))))
	}

	DescribeTable("rejecting invalid parameters",
		func(query string, errtext string) {
			Expect(parse("/capture" + query)).Error().To(MatchError(ContainSubstring(errtext)))
		},
		Entry("no target", "", "either container or netns query parameter required"),
		Entry("container and netns", "?netns=42&container="+url.QueryEscape("{}"), "mutually exclusive"),
		Entry("invalid container description", "?container=foo", "invalid container/target description"),
		Entry("non-numeric netns", "?netns=foo", `invalid netns "foo"`),
		Entry("zero netns", "?netns=0", `invalid netns "0"`),
		Entry("negative netns", "?netns=-1", `invalid netns "-1"`),
	)

	It("captures from all network interfaces of a bare network namespace", func() {
		args := Successful(parse(fmt.Sprintf("/capture?netns=%d", ownnetns)))
		Expect(args.NetNS).To(Equal(ownnetns))
		Expect(args.NetworkInterfaces).To(ConsistOf("any"))
		Expect(args.CaptureFilter).To(BeEmpty())
		Expect(args.KeepChaste).To(BeFalse())
	})

	It("takes network interfaces, filter, and chaste mode from query parameters", func() {
		args := Successful(parse(fmt.Sprintf("/capture?netns=%d&nif=eth0/eth1&filter=tcp+port+80&chaste", ownnetns)))
		Expect(args.NetworkInterfaces).To(ConsistOf("eth0", "eth1"))
		Expect(args.CaptureFilter).To(Equal("tcp port 80"))
		Expect(args.KeepChaste).To(BeTrue())
	})

	It("keeps the target's network interfaces for nif=any", func() {
		args := Successful(parse("/capture?nif=any&container=" + containerParam(api.Target{
			Name:              "foo",
			NetNS:             ownnetns,
			NetworkInterfaces: []string{"eth0", "lo"},
		})))
		Expect(args.NetworkInterfaces).To(ConsistOf("eth0", "lo"))
	})

	It("prefers the Clustershark headers over URL query parameters", func() {
		args := Successful(parse("/capture?nif=eth0&filter=udp",
			"Clustershark-Container", string(Successful(json.Marshal(api.Target{Name: "foo", NetNS: ownnetns}))),
			"Clustershark-Nif", "eth1",
			"Clustershark-Filter", "tcp",
			"Clustershark-Chaste", "1"))
		Expect(args.Name).To(Equal("foo"))
		Expect(args.NetNS).To(Equal(ownnetns))
		Expect(args.NetworkInterfaces).To(ConsistOf("eth1"))
		Expect(args.CaptureFilter).To(Equal("tcp"))
		Expect(args.KeepChaste).To(BeTrue())
	})

	It("keeps a current target description", func() {
		args := Successful(parse("/capture?container=" + containerParam(api.Target{
			Name:      "foo",
			NetNS:     ownnetns,
			Pid:       os.Getpid(),
			StartTime: startTime(os.Getpid()),
		})))
		Expect(args.NetNS).To(Equal(ownnetns))
	})

	It("verifies the start times of processes with closing parentheses in their names", func() {
		tmpdir := Successful(os.MkdirTemp("", "packetflix-test-"))
		DeferCleanup(os.RemoveAll, tmpdir)
		sleeper := filepath.Join(tmpdir, "x) 1 2")
		Expect(os.WriteFile(sleeper, Successful(os.ReadFile("/bin/sleep")), 0o755)).To(Succeed())
		cmd := exec.Command(sleeper, "60")
		Expect(cmd.Start()).To(Succeed())
		DeferCleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})

		args := Successful(parse("/capture?container=" + containerParam(api.Target{
			Name:      "foo",
			NetNS:     ownnetns,
			Pid:       cmd.Process.Pid,
			StartTime: startTime(cmd.Process.Pid),
		})))
		Expect(args.NetNS).To(Equal(ownnetns))
	})

	When("the target description is stale", func() {

		Context("with a working discovery service", func() {

			BeforeEach(func() {
				targets := Successful(json.Marshal(api.GwTargetList{Targets: api.Targets{
					{Name: "foo", Type: "docker", NetNS: ownnetns, NetworkInterfaces: []string{"eth0", "lo"}},
					{Name: "foo", Prefix: "other", Type: "docker", NetNS: 666},
				}}))
				discovery = fakeDiscovery(func(w http.ResponseWriter, req *http.Request) {
					defer GinkgoRecover()
					Expect(req.URL.Path).To(Equal("/mobyshark"))
					_, _ = w.Write(targets)
				})
			})

			It("refreshes it when the process is gone", func() {
				args := Successful(parse("/capture?container=" + containerParam(api.Target{
					Name:      "foo",
					NetNS:     42,
					Pid:       1<<31 - 1,
					StartTime: 1,
				})))
				Expect(args.NetNS).To(Equal(ownnetns))
				Expect(args.Type).To(Equal("docker"))
				Expect(args.NetworkInterfaces).To(ConsistOf("eth0", "lo"))
			})

			It("refreshes it when the process start time differs", func() {
				args := Successful(parse("/capture?container=" + containerParam(api.Target{
					Name:      "foo",
					NetNS:     42,
					Pid:       os.Getpid(),
					StartTime: startTime(os.Getpid()) + 1,
				})))
				Expect(args.NetNS).To(Equal(ownnetns))
			})

			It("keeps the originally requested network interfaces", func() {
				args := Successful(parse("/capture?container=" + containerParam(api.Target{
					Name:              "foo",
					NetworkInterfaces: []string{"eth0"},
				})))
				Expect(args.NetNS).To(Equal(ownnetns))
				Expect(args.NetworkInterfaces).To(ConsistOf("eth0"))
			})

			It("matches targets also by their prefix", func() {
				args := Successful(parse("/capture?container=" + containerParam(api.Target{
					Name:   "foo",
					Prefix: "other",
				})))
				Expect(args.NetNS).To(Equal(666))
			})

		})

		It("fails when the discovery service cannot be reached", func() {
			l := Successful(net.Listen("tcp", "127.0.0.1:0"))
			discovery = l.Addr().String()
			Expect(l.Close()).To(Succeed())

			Expect(parse("/capture?container=" + containerParam(api.Target{Name: "foo"}))).Error().
				To(MatchError(ContainSubstring("cannot update container meta data")))
		})

		It("fails on garbled discovery data", func() {
			discovery = fakeDiscovery(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("garbage"))
			})
			Expect(parse("/capture?container=" + containerParam(api.Target{Name: "foo"}))).Error().
				To(MatchError(ContainSubstring("cannot update container/target meta data")))
		})

	})

})
