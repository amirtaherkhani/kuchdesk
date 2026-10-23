package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/deploy"
)

func run(args []string, stdout, stderr io.Writer, runner deploy.Runner) int {
	return runWithPreflight(args, stdout, stderr, runner, nil)
}

func runWithPreflight(args []string, stdout, stderr io.Writer, runner deploy.Runner, preflight deploy.Preflight) int {
	flags := flag.NewFlagSet("kuchdesk-deploy", flag.ContinueOnError)
	flags.SetOutput(stderr)
	profile := flags.String("profile", "", "absolute path to a deployment profile JSON file")
	apply := flags.Bool("apply", false, "run the planned commands")
	identityOnly := flags.Bool("identity", false, "report current source/build input identity without building")
	preflightOnly := flags.Bool("preflight", false, "run read-only deployment checks without building")
	confirm := flags.String("confirm", "", "exact release/namespace confirmation for apply")
	timeout := flags.Duration("timeout", 20*time.Minute, "overall execution timeout")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || !filepath.IsAbs(*profile) || *timeout < time.Minute || *timeout > 2*time.Hour || (*apply && *preflightOnly) || (*identityOnly && (*apply || *preflightOnly)) {
		fmt.Fprintln(stderr, "usage: kuchdesk-deploy -profile /absolute/profile.json [-identity | -preflight | -apply -confirm release/namespace] [-timeout 20m]")
		return 2
	}
	spec, err := deploy.LoadSpec(*profile)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if *identityOnly {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		identity, err := deploy.SourceIdentity(ctx, runner, spec)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(identity); err != nil {
			fmt.Fprintln(stderr, "write identity failed")
			return 1
		}
		return 0
	}
	if *preflightOnly {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		report, checkErr := (deploy.Workflow{Preflight: preflight}).Check(ctx, spec)
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			fmt.Fprintln(stderr, "write preflight report failed")
			return 1
		}
		if checkErr != nil {
			fmt.Fprintln(stderr, checkErr)
			return 1
		}
		return 0
	}
	steps, err := spec.Plan()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if !*apply {
		if err := json.NewEncoder(stdout).Encode(map[string]any{"release": spec.Release, "namespace": spec.Namespace, "kubeContext": spec.KubeContext, "sourceDir": spec.SourceDir, "chartDir": spec.ChartDir, "valuesFiles": spec.ValuesFiles, "deployment": spec.Deployment, "image": spec.ImageRepository + ":" + spec.ImageTag, "platform": spec.Platform, "buildArgs": spec.BuildArgs, "reuseBuild": spec.ReuseBuild, "rebuild": spec.Rebuild, "refreshImages": spec.RefreshImages, "preflightDomain": spec.PreflightDomain, "preflightRegistryUrl": spec.PreflightRegistryURL, "steps": steps}); err != nil {
			fmt.Fprintln(stderr, "write plan failed")
			return 1
		}
		return 0
	}
	if *confirm != spec.Release+"/"+spec.Namespace {
		fmt.Fprintln(stderr, "exact release/namespace confirmation required")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := (deploy.Workflow{Runner: runner, Preflight: preflight, OnStep: func(step string) { fmt.Fprintln(stderr, step) }}).Apply(ctx, spec)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, "write result failed")
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, deploy.ExecRunner{}))
}
