package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Command struct {
	Program string   `json:"program"`
	Args    []string `json:"args"`
}

type DiscoveryCommand struct {
	Command
	TargetPath string `json:"targetPath"`
	LabelPath  string `json:"labelPath,omitempty"`
}

type Profile struct {
	Name     string           `json:"name"`
	Discover DiscoveryCommand `json:"discover"`
	Users    []string         `json:"users"`
	Connect  Command          `json:"connect"`
	Upload   Command          `json:"upload"`
	Download Command          `json:"download"`
	MaxBytes int64            `json:"maxTransferBytes,omitempty"`
}

type Config struct {
	Providers []Profile `json:"providers"`
}

type Target struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Users []string `json:"users"`
}

type Discovery struct {
	Targets []Target `json:"targets"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("provider config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate() error {
	seen := make(map[string]struct{}, len(c.Providers))
	for _, profile := range c.Providers {
		if profile.Name == "" {
			return errors.New("provider config: provider name is required")
		}
		if _, ok := seen[profile.Name]; ok {
			return fmt.Errorf("provider config: duplicate provider %q", profile.Name)
		}
		seen[profile.Name] = struct{}{}
		for name, command := range map[string]Command{
			"discover": profile.Discover.Command,
			"connect":  profile.Connect,
			"upload":   profile.Upload,
			"download": profile.Download,
		} {
			if command.Program == "" {
				return fmt.Errorf("provider config: %s command is required for %q", name, profile.Name)
			}
		}
		if profile.Discover.TargetPath == "" {
			return fmt.Errorf("provider config: discover.targetPath is required for %q", profile.Name)
		}
		if len(profile.Users) == 0 {
			return fmt.Errorf("provider config: users are required for %q", profile.Name)
		}
	}
	return nil
}

func (p Profile) Run(ctx context.Context, command Command, values map[string]string, input []byte) ([]byte, error) {
	args, err := expandArgs(command.Args, values)
	if err != nil {
		return nil, err
	}
	process := exec.CommandContext(ctx, command.Program, args...)
	if input != nil {
		process.Stdin = strings.NewReader(string(input))
	}
	output, err := process.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("provider command %q: %w", command.Program, err)
	}
	return output, nil
}

func (p Profile) DiscoverTargets(ctx context.Context) (Discovery, error) {
	output, err := p.Run(ctx, p.Discover.Command, nil, nil)
	if err != nil {
		return Discovery{}, err
	}
	var raw any
	if err := json.Unmarshal(output, &raw); err != nil {
		return Discovery{}, fmt.Errorf("provider %q discovery: %w", p.Name, err)
	}
	if object, ok := raw.(map[string]any); ok {
		if targets, ok := object["targets"]; ok {
			raw = targets
		}
	}
	var values []map[string]any
	data, _ := json.Marshal(raw)
	if err := json.Unmarshal(data, &values); err != nil {
		return Discovery{}, fmt.Errorf("provider %q discovery: %w", p.Name, err)
	}
	discovery := Discovery{Targets: make([]Target, 0, len(values))}
	for _, value := range values {
		id, ok := stringPath(value, p.Discover.TargetPath)
		if !ok || id == "" {
			continue
		}
		label := id
		if p.Discover.LabelPath != "" {
			if mapped, ok := stringPath(value, p.Discover.LabelPath); ok && mapped != "" {
				label = mapped
			}
		}
		discovery.Targets = append(discovery.Targets, Target{ID: id, Label: label, Users: append([]string(nil), p.Users...)})
	}
	return discovery, nil
}

func stringPath(value map[string]any, path string) (string, bool) {
	var current any = value
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current, ok = object[part]
		if !ok {
			return "", false
		}
	}
	text, ok := current.(string)
	return text, ok
}

func (p Profile) Target(id string, discovery Discovery) (Target, bool) {
	for _, target := range discovery.Targets {
		if target.ID == id {
			return target, true
		}
	}
	return Target{}, false
}

func expandArgs(args []string, values map[string]string) ([]string, error) {
	result := make([]string, len(args))
	for i, arg := range args {
		result[i] = arg
		for key, value := range values {
			result[i] = strings.ReplaceAll(result[i], "{"+key+"}", value)
		}
		if strings.Contains(result[i], "{") {
			return nil, fmt.Errorf("provider command: unresolved placeholder in argument %q", arg)
		}
	}
	return result, nil
}
