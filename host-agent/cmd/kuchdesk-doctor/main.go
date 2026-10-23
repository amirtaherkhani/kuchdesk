package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/doctor"
)

type targets []doctor.Target

func (t *targets) String() string { return fmt.Sprint(*t) }
func (t *targets) Set(value string) error {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return fmt.Errorf("service must be namespace/deployment")
	}
	*t = append(*t, doctor.Target{Namespace: parts[0], Deployment: parts[1]})
	return nil
}

func run(args []string, stdout, stderr io.Writer, service doctor.Service) int {
	flags := flag.NewFlagSet("kuchdesk-doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	domain := flags.String("domain", "", "DNS name to resolve on this Mac")
	kubeContext := flags.String("kube-context", "", "explicit Kubernetes context")
	registryURL := flags.String("registry-url", "", "loopback registry HTTP origin")
	diskPath := flags.String("disk-path", "", "absolute path of the build volume")
	minFreeGiB := flags.Uint64("min-free-gib", 2, "minimum free disk space in GiB")
	var services targets
	flags.Var(&services, "service", "deployment to check as namespace/name; repeatable")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *minFreeGiB > 1024 {
		fmt.Fprintln(stderr, "usage: kuchdesk-doctor -domain name -kube-context context -disk-path /absolute/path [-registry-url LOOPBACK_ORIGIN] [-service namespace/deployment]")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, err := service.Run(ctx, doctor.Config{Domain: *domain, KubeContext: *kubeContext, RegistryURL: *registryURL, DiskPath: *diskPath, MinFreeBytes: *minFreeGiB << 30, Services: services})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		fmt.Fprintln(stderr, "write Doctor report failed")
		return 1
	}
	if !report.Healthy {
		return 1
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, doctor.Service{})) }
