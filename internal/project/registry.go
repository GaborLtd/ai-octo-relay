package project

import (
	"fmt"
	"slices"
	"sort"

	"github.com/match/ai-octo-relay/internal/config"
)

type Project struct {
	Name         string
	Path         string
	DefaultAgent string
}

type Registry struct {
	projects map[string]Project
	names    []string
	channels map[string]string
}

func NewRegistry(configs []config.ProjectConfig) (*Registry, error) {
	projects := make(map[string]Project, len(configs))
	names := make([]string, 0, len(configs))
	channels := make(map[string]string)
	for _, cfg := range configs {
		if _, exists := projects[cfg.Name]; exists {
			return nil, fmt.Errorf("duplicate project name: %s", cfg.Name)
		}
		projects[cfg.Name] = Project{
			Name:         cfg.Name,
			Path:         cfg.Path,
			DefaultAgent: cfg.DefaultAgent,
		}
		for _, channelID := range cfg.ChannelIDs {
			if existing, ok := channels[channelID]; ok {
				return nil, fmt.Errorf("channel %s is assigned to multiple projects: %s, %s", channelID, existing, cfg.Name)
			}
			channels[channelID] = cfg.Name
		}
		names = append(names, cfg.Name)
	}
	sort.Strings(names)
	return &Registry{projects: projects, names: names, channels: channels}, nil
}

func (r *Registry) Get(name string) (Project, bool) {
	project, ok := r.projects[name]
	return project, ok
}

func (r *Registry) List() []Project {
	out := make([]Project, 0, len(r.names))
	for _, name := range r.names {
		out = append(out, r.projects[name])
	}
	return out
}

func (r *Registry) Names() []string {
	return slices.Clone(r.names)
}

func (r *Registry) ProjectForChannel(channelID string) (Project, bool) {
	name, ok := r.channels[channelID]
	if !ok {
		return Project{}, false
	}
	project, ok := r.projects[name]
	return project, ok
}
