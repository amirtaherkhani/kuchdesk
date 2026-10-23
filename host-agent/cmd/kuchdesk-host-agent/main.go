package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/amirtaherkhani/kuchdesk/configyaml"
	"gopkg.in/yaml.v3"
)

var portPattern = regexp.MustCompile(`Forwarding from 127\.0\.0\.1:(\d+) -> (\d+)`)

type apiResult struct {
	Status       string          `json:"status"`
	ErrorMessage string          `json:"errorMessage"`
	Token        string          `json:"token"`
	Response     json.RawMessage `json:"response"`
}

type apiClient struct {
	base  string
	token string
	http  *http.Client
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "grafana-admin-password" || os.Args[1] == "grafana-admin-password-reset") {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		var err error
		if os.Args[1] == "grafana-admin-password" {
			err = runGrafanaAdminPassword(ctx, os.Args[2:], os.Stdout, openGrafanaTerminal, readGrafanaBootstrapPassword)
		} else {
			err = runGrafanaAdminPasswordReset(ctx, os.Args[2:], os.Stdout, openGrafanaTerminal, readGrafanaHiddenPassword, resetGrafanaAdminPassword)
		}
		stop()
		if err != nil {
			fmt.Fprintln(os.Stderr, "host-agent Grafana password command failed:", err)
			os.Exit(1)
		}
		return
	}
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "host-agent configuration failed:", err)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	if cfg.CheckTarget {
		ip, err := targetIP(ctx, cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "host-agent target check failed:", err)
			os.Exit(1)
		}
		fmt.Println(ip)
		return
	}
	if err := reconcile(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "host-agent DNS reconciliation failed:", err)
		os.Exit(1)
	}
}

func reconcile(ctx context.Context, cfg config) error {
	if _, _, err := chartvalues.Path(cfg.ValuesRoot, "technitium", "values.local.yaml"); err != nil {
		return errors.New("Technitium canonical private YAML is required")
	}
	ip, err := targetIP(ctx, cfg)
	if err != nil {
		return err
	}
	password, err := adminPassword(cfg)
	if err != nil {
		return err
	}
	port, stop, err := forwardAPI(ctx, cfg)
	if err != nil {
		return err
	}
	defer stop()
	client := apiClient{base: "http://127.0.0.1:" + port, http: &http.Client{Timeout: 8 * time.Second}}
	login, err := client.call(ctx, "/api/user/login", url.Values{"user": {cfg.AdminUser}, "pass": {password}})
	if err != nil {
		return fmt.Errorf("Technitium login: %w", err)
	}
	client.token = login.Token
	if client.token == "" {
		return errors.New("Technitium login returned no token")
	}
	defer func() { _, _ = client.call(context.Background(), "/api/user/logout", url.Values{}) }()
	return reconcileDNS(ctx, &client, cfg, ip)
}

func targetIP(_ context.Context, cfg config) (net.IP, error) {
	if cfg.TargetIP == "" {
		return nil, errors.New("explicit -target-ip must name the verified Colima ingress address; the Mac LAN address is not inferred")
	}
	return net.ParseIP(cfg.TargetIP).To4(), nil
}

func reconcileDNS(ctx context.Context, client *apiClient, cfg config, ip net.IP) error {
	zones, err := client.call(ctx, "/api/zones/list", nil)
	if err != nil {
		return err
	}
	var zoneList struct {
		Zones []struct {
			Name string `json:"name"`
		} `json:"zones"`
	}
	if err := json.Unmarshal(zones.Response, &zoneList); err != nil {
		return err
	}
	found := false
	for _, z := range zoneList.Zones {
		if strings.EqualFold(z.Name, cfg.Zone) {
			found = true
			break
		}
	}
	if !found {
		if _, err := client.call(ctx, "/api/zones/create", url.Values{"zone": {cfg.Zone}, "type": {"Primary"}}); err != nil {
			return err
		}
	}

	records, err := client.call(ctx, "/api/zones/records/get?"+url.Values{"domain": {cfg.recordName()}, "zone": {cfg.Zone}}.Encode(), nil)
	if err != nil {
		return err
	}
	var recordList struct {
		Records []struct {
			Name  string `json:"name"`
			Type  string `json:"type"`
			TTL   int    `json:"ttl"`
			RData struct {
				IPAddress string `json:"ipAddress"`
			} `json:"rData"`
		} `json:"records"`
	}
	if err := json.Unmarshal(records.Response, &recordList); err != nil {
		return err
	}
	for _, r := range recordList.Records {
		if strings.EqualFold(r.Name, cfg.recordName()) && r.Type == "A" && r.TTL == cfg.TTL && r.RData.IPAddress == ip.String() {
			fmt.Println("Technitium wildcard current:", ip)
			return nil
		}
	}
	_, err = client.call(ctx, "/api/zones/records/add", url.Values{
		"domain": {cfg.recordName()}, "zone": {cfg.Zone}, "type": {"A"}, "ttl": {strconv.Itoa(cfg.TTL)}, "overwrite": {"true"}, "ipAddress": {ip.String()},
	})
	if err != nil {
		return err
	}
	fmt.Println("Technitium wildcard updated:", ip)
	return nil
}

func adminPassword(cfg config) (string, error) {
	data, _, err := chartvalues.Read(cfg.ValuesRoot, "technitium", "values.local.yaml")
	if err != nil {
		return "", errors.New("read Technitium local values: canonical private YAML unavailable")
	}
	defer clear(data)
	var values struct {
		AdminPassword string `yaml:"adminPassword"`
	}
	if err := yaml.Unmarshal(data, &values); err != nil || values.AdminPassword == "" {
		return "", errors.New("Technitium local password missing or invalid")
	}
	return values.AdminPassword, nil
}

func forwardAPI(parent context.Context, cfg config) (string, func(), error) {
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, cfg.Kubectl, cfg.kubectlArgs("port-forward", "--address", "127.0.0.1", "svc/"+cfg.Service, ":"+strconv.Itoa(cfg.APIPort))...)
	reader, writer := io.Pipe()
	cmd.Stdout, cmd.Stderr = writer, writer
	if err := cmd.Start(); err != nil {
		cancel()
		return "", nil, err
	}
	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			default:
				// Port-forward emits connection lines after startup; never block its pipe.
			case <-ctx.Done():
				return
			}
		}
		close(lines)
	}()
	stop := func() { cancel(); _ = cmd.Wait(); _ = writer.Close(); _ = reader.Close() }
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				stop()
				return "", nil, errors.New("kubectl port-forward exited")
			}
			if matches := portPattern.FindStringSubmatch(line); len(matches) == 3 && matches[2] == strconv.Itoa(cfg.APIPort) {
				return matches[1], stop, nil
			}
		case <-timer.C:
			stop()
			return "", nil, errors.New("timed out starting Technitium port-forward")
		case <-parent.Done():
			stop()
			return "", nil, parent.Err()
		}
	}
}

func (c *apiClient) call(ctx context.Context, path string, form url.Values) (apiResult, error) {
	var body io.Reader
	method := http.MethodGet
	if form != nil {
		method, body = http.MethodPost, bytes.NewBufferString(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return apiResult{}, err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return apiResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return apiResult{}, fmt.Errorf("Technitium %s: HTTP %d", path, resp.StatusCode)
	}
	var result apiResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return apiResult{}, err
	}
	if result.Status != "ok" {
		return apiResult{}, fmt.Errorf("Technitium %s: %s", path, result.ErrorMessage)
	}
	return result, nil
}
