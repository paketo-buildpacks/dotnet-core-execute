package dotnetexecute

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Netflix/go-env"
	"github.com/paketo-buildpacks/packit/v2"
	"github.com/paketo-buildpacks/packit/v2/chronos"
	"github.com/paketo-buildpacks/packit/v2/sbom"
	"github.com/paketo-buildpacks/packit/v2/scribe"
)

//go:generate faux --interface SBOMGenerator --output fakes/sbom_generator.go
type SBOMGenerator interface {
	Generate(path string) (sbom.SBOM, error)
}

// Build will return a packit.BuildFunc that will be invoked during the build
// phase of the buildpack lifecycle.
//
// Build generates a SBOM of the .NET app's dependencies based on its compiled
// DLLs. It sets up the entrypoint for the app image and adds a helper that
// will determine at launch-time which container port the app should listen on.
func Build(
	config Configuration,
	configParser ConfigParser,
	sbomGenerator SBOMGenerator,
	logger scribe.Emitter,
	clock chronos.Clock,
) packit.BuildFunc {
	return func(context packit.BuildContext) (packit.BuildResult, error) {
		logger.Title("%s %s", context.BuildpackInfo.Name, context.BuildpackInfo.Version)

		es, err := env.Marshal(&config)
		if err != nil {
			// not tested
			return packit.BuildResult{}, fmt.Errorf("parsing build configuration: %w", err)
		}

		logger.Debug.Process("Build configuration:")
		for envVar := range es {
			// for bug https://github.com/Netflix/go-env/issues/23
			if !strings.Contains(envVar, "=") {
				logger.Debug.Subprocess("%s: %s", envVar, es[envVar])
			}
		}
		logger.Debug.Break()

		runtimeConfig, err := configParser.Parse(filepath.Join(context.WorkingDir, "*.runtimeconfig.json"))
		if err != nil {
			return packit.BuildResult{}, fmt.Errorf("failed to find *.runtimeconfig.json: %w", err)
		}

		logger.GeneratingSBOM(context.WorkingDir)
		var sbomContent sbom.SBOM
		duration, err := clock.Measure(func() error {
			sbomContent, err = sbomGenerator.Generate(context.WorkingDir)
			return err
		})
		if err != nil {
			return packit.BuildResult{}, err
		}

		logger.Action("Completed in %s", duration.Round(time.Millisecond))
		logger.Break()

		logger.FormattingSBOM(context.BuildpackInfo.SBOMFormats...)
		sbomFormatter, err := sbomContent.InFormats(context.BuildpackInfo.SBOMFormats...)
		if err != nil {
			return packit.BuildResult{}, err
		}

		command := filepath.Join(context.WorkingDir, runtimeConfig.AppName)
		var args []string
		if !runtimeConfig.Executable {
			_, err := os.Stat(filepath.Join(context.WorkingDir, fmt.Sprintf("%s.dll", runtimeConfig.AppName)))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return packit.BuildResult{}, err
			}
			if errors.Is(err, os.ErrNotExist) {
				return packit.BuildResult{}, fmt.Errorf("no entrypoint [%s.dll] found: %w ", runtimeConfig.AppName, err)
			}

			command = "dotnet"
			args = append(args, fmt.Sprintf("%s.dll", filepath.Join(context.WorkingDir, runtimeConfig.AppName)))
		}

		processes := []packit.Process{
			{
				Type:    runtimeConfig.AppName,
				Command: command,
				Args:    args,
				Default: true,
				Direct:  true,
			},
		}

		if config.LiveReloadEnabled {
			processes = []packit.Process{
				{
					Type:    fmt.Sprintf("reload-%s", runtimeConfig.AppName),
					Command: "watchexec",
					Args: append([]string{
						"--restart",
						"--watch", context.WorkingDir,
						"--shell", "none",
						"--",
						command,
					}, args...),
					Default: true,
					Direct:  true,
				},
				{
					Type:    runtimeConfig.AppName,
					Command: command,
					Args:    args,
					Direct:  true,
				},
			}

			err := filepath.Walk(context.WorkingDir, func(path string, info fs.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if path == context.WorkingDir {
					return nil
				}

				return os.Chmod(path, info.Mode()|0060)
			})
			if err != nil {
				return packit.BuildResult{}, err
			}
		}

		logger.LaunchProcesses(processes)

		portChooserLayer, err := context.Layers.Get("port-chooser")
		if err != nil {
			return packit.BuildResult{}, err
		}
		portChooserLayer.Launch = true
		portChooserLayer.ExecD = []string{filepath.Join(context.CNBPath, "bin", "port-chooser")}

		if config.DebugEnabled {
			portChooserLayer.LaunchEnv.Default("ASPNETCORE_ENVIRONMENT", "Development")
		}

		logger.LayerFlags(portChooserLayer)
		logger.EnvironmentVariables(portChooserLayer)

		return packit.BuildResult{
			Layers: []packit.Layer{
				portChooserLayer,
			},
			Launch: packit.LaunchMetadata{
				Processes: processes,
				SBOM:      sbomFormatter,
			},
		}, nil
	}
}
