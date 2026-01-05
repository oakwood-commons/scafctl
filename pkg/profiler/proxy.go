package profiler

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/pprof/profile"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

var (
	profilerOnce sync.Once
	instance     *Proxy
)

func concreteProfiler(profileType string) (runtimeProfiler, error) {
	switch profileType {
	case "cpu":
		return &cpuProfiler{fileManager: &fileManager{}}, nil
	case "memory":
		return &memoryProfiler{fileManager: &fileManager{}}, nil
	case "goroutine":
		return &goroutineProfiler{fileManager: &fileManager{}}, nil
	default:
		return nil, fmt.Errorf("unsupported profiler type: %s", profileType)
	}
}

var allowedProfilers = map[string]bool{"memory": true, "cpu": true, "goroutine": true}

func validateRunTimeProfiler(profileType string) error {
	if !allowedProfilers[profileType] {
		return fmt.Errorf("invalid value for --profiler: %s (allowed: memory, cpu, goroutine)", profileType)
	}
	return nil
}

func GetProfiler(profileType, path string, lgr *logr.Logger) (*Proxy, error) {
	err := validateRunTimeProfiler(profileType)
	if err != nil {
		lgr.Error(err, "Error validating profiler type")
		return nil, fmt.Errorf("error validating profiler type: %w", err)
	}

	if path == "" {
		path = "./"
	}

	profilerOnce.Do(func() {
		var profiler runtimeProfiler

		profiler, err = concreteProfiler(profileType)
		if err != nil {
			return
		}
		lgr.V(1).Info("Creating profiler", "type", profileType, "path", path)
		instance = &Proxy{profileType: profileType, path: path, profiler: profiler, stopCh: make(chan struct{})}
	})

	if err != nil {
		lgr.Error(err, "Error creating profiler")
		return nil, fmt.Errorf("error creating profiler: %w", err)
	}

	return instance, err
}

func (p *Proxy) Start(lgr *logr.Logger) error {
	for {
		select {
		case <-p.stopCh:
			return nil
		default:
			lgr.V(1).Info("Starting profiler", "type", p.profileType)
			err := p.profiler.Start()
			if err != nil {
				lgr.Error(err, "Error starting profiler")
				return fmt.Errorf("error starting profiler: %w", err)
			}
		}
		time.Sleep(1 * time.Second)
	}
}

func (p *Proxy) writeMergedProfile(profile *profile.Profile) error {
	if profile == nil {
		return fmt.Errorf("no profile data to write")
	}

	// Construct the file path using OS-aware path joining
	filePath := filepath.Join(p.path, fmt.Sprintf("%s.prof", p.profileType))

	// Create the file
	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("could not create profile file: %w", err)
	}
	defer f.Close()

	// Write the merged profile to the file
	if err := profile.Write(f); err != nil {
		return fmt.Errorf("failed to write merged profile: %w", err)
	}

	p.finalPath = filePath

	return nil
}

func (p *Proxy) Stop(lgr *logr.Logger) error {
	close(p.stopCh)

	defer func() {
		lgr.V(1).Info("Cleaning up profiler files")
		p.profiler.cleanUp()
	}()

	lgr.V(1).Info("Stopping profiler")
	err := p.profiler.Stop()
	if err != nil {
		lgr.Error(err, "Error stopping profiler")
		return fmt.Errorf("error stopping profiler: %w", err)
	}

	lgr.V(1).Info("Profiler stopped, merging profiles")
	profile, err := p.mergeProfiles()
	if err != nil {
		lgr.Error(err, "Error merging profiles")
		return fmt.Errorf("error merging profiles: %w", err)
	}

	if err := p.writeMergedProfile(profile); err != nil {
		lgr.Error(err, "Error writing merged profile")
		return fmt.Errorf("error writing merged profile: %w", err)
	}

	lgr.V(1).Info("Merged profile written", "file", p.finalPath)

	return nil
}

func (p *Proxy) mergeProfiles() (*profile.Profile, error) {
	if len(p.getFiles()) == 0 {
		return nil, nil
	}

	profiles := make([]*profile.Profile, 0, len(p.getFiles()))

	for _, f := range p.getFiles() {
		if _, err := f.Seek(0, 0); err != nil {
			return nil, fmt.Errorf("failed to seek temp file %s: %w", f.Name(), err)
		}

		profile, err := profile.Parse(f)
		if err != nil {
			return nil, fmt.Errorf("failed to parse profile from file %s: %w", f.Name(), err)
		}

		profiles = append(profiles, profile)
	}

	mergedProfile, err := profile.Merge(profiles)
	if err != nil {
		return nil, fmt.Errorf("failed to merge profiles: %w", err)
	}

	return mergedProfile, nil
}

func (p *Proxy) getFiles() []*os.File {
	return p.profiler.getFiles()
}

func StopProfiler() error {
	if instance == nil {
		return nil
	}

	lgr := logger.Get(0)
	err := instance.Stop(lgr)
	if err != nil {
		lgr.Error(err, "Error stopping profiler")
		return fmt.Errorf("error stopping profiler: %w", err)
	}

	// reset singleton
	instance = nil
	profilerOnce = sync.Once{}

	return nil
}
