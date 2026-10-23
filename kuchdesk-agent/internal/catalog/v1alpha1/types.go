// Package v1alpha1 defines the declarative service-module contract.
// It does not grant cluster write permissions or load plugins.
package v1alpha1

import (
	"fmt"

	"sigs.k8s.io/yaml"
)

const APIVersion = "catalog.kuchdesk.io/v1alpha1"

type ServiceModule struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Module     ModuleMetadata `json:"module"`
	Components []Component    `json:"components"`
}

type ModuleMetadata struct {
	ID            string   `json:"id"`
	DisplayName   string   `json:"displayName"`
	Category      string   `json:"category"`
	Ownership     string   `json:"ownership"`
	Purpose       string   `json:"purpose"`
	Documentation string   `json:"documentation,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

type Component struct {
	ID                  string               `json:"id"`
	Type                string               `json:"type"`
	Purpose             string               `json:"purpose"`
	Provides            []string             `json:"provides,omitempty"`
	Requires            []string             `json:"requires,omitempty"`
	ExternalConnections []ExternalConnection `json:"externalConnections,omitempty"`
	WorkloadRefs        []WorkloadRef        `json:"workloadRefs,omitempty"`
	Endpoints           []Endpoint           `json:"endpoints,omitempty"`
	HealthChecks        []HealthCheck        `json:"healthChecks,omitempty"`
	Observability       Observability        `json:"observability,omitempty"`
}

type ExternalConnection struct {
	Name      string `json:"name"`
	TargetRef string `json:"targetRef"`
	Protocol  string `json:"protocol,omitempty"`
}

type WorkloadRef struct {
	APIVersion    string            `json:"apiVersion"`
	Kind          string            `json:"kind"`
	Name          string            `json:"name"`
	Namespace     string            `json:"namespace"`
	LabelSelector map[string]string `json:"labelSelector,omitempty"`
}

type Endpoint struct {
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`
	AddressRef string `json:"addressRef"`
	PortName   string `json:"portName,omitempty"`
}

type HealthCheck struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	TargetRef      string `json:"targetRef"`
	Path           string `json:"path,omitempty"`
	ExpectedStatus int    `json:"expectedStatus,omitempty"`
}

type Observability struct {
	Dashboards []string `json:"dashboards,omitempty"`
	Metrics    []string `json:"metrics,omitempty"`
	Logs       []string `json:"logs,omitempty"`
}

type InstallationProfile struct {
	APIVersion    string         `json:"apiVersion"`
	Kind          string         `json:"kind"`
	Profile       ProfileInfo    `json:"profile"`
	Installations []Installation `json:"installations"`
}

type ProfileInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Target      string `json:"target"`
}

type Installation struct {
	ModuleID    string   `json:"moduleId"`
	ReleaseName string   `json:"releaseName"`
	Namespace   string   `json:"namespace"`
	Chart       ChartRef `json:"chart"`
	ValuesFiles []string `json:"valuesFiles,omitempty"`
}

type ChartRef struct {
	Source    string `json:"source"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	LocalPath string `json:"localPath,omitempty"`
}

func ParseServiceModule(data []byte) (ServiceModule, error) {
	var module ServiceModule
	if err := yaml.UnmarshalStrict(data, &module); err != nil {
		return ServiceModule{}, fmt.Errorf("decode service module: %w", err)
	}
	if module.APIVersion != APIVersion || module.Kind != "ServiceModule" {
		return ServiceModule{}, fmt.Errorf("unsupported service module version or kind")
	}
	if module.Module.ID == "" || module.Module.DisplayName == "" || module.Module.Category == "" || module.Module.Purpose == "" ||
		(module.Module.Ownership != "managed" && module.Module.Ownership != "external") || len(module.Components) == 0 {
		return ServiceModule{}, fmt.Errorf("incomplete service module metadata")
	}
	seen := make(map[string]struct{}, len(module.Components))
	for _, component := range module.Components {
		if component.ID == "" || component.Type == "" || component.Purpose == "" {
			return ServiceModule{}, fmt.Errorf("incomplete service component metadata")
		}
		if _, ok := seen[component.ID]; ok {
			return ServiceModule{}, fmt.Errorf("duplicate component id %q", component.ID)
		}
		seen[component.ID] = struct{}{}
	}
	return module, nil
}

func ParseInstallationProfile(data []byte) (InstallationProfile, error) {
	var profile InstallationProfile
	if err := yaml.UnmarshalStrict(data, &profile); err != nil {
		return InstallationProfile{}, fmt.Errorf("decode installation profile: %w", err)
	}
	if profile.APIVersion != APIVersion || profile.Kind != "InstallationProfile" || profile.Profile.Target != "local-single-node" {
		return InstallationProfile{}, fmt.Errorf("unsupported installation profile version, kind, or target")
	}
	if profile.Profile.ID == "" || profile.Profile.DisplayName == "" {
		return InstallationProfile{}, fmt.Errorf("incomplete installation profile metadata")
	}
	return profile, nil
}
