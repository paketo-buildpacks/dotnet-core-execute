package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/paketo-buildpacks/occam"
	"github.com/sclevine/spec"

	. "github.com/onsi/gomega"
	. "github.com/paketo-buildpacks/occam/matchers"
)

func testFrameworkDependentExecutable(t *testing.T, context spec.G, it spec.S) {
	var (
		Expect     = NewWithT(t).Expect
		Eventually = NewWithT(t).Eventually

		pack   occam.Pack
		docker occam.Docker
	)

	it.Before(func() {
		pack = occam.NewPack().WithVerbose().WithNoColor()
		docker = occam.NewDocker()
	})

	context("when building a default app", func() {
		var (
			image     occam.Image
			container occam.Container

			name   string
			source string
		)

		it.Before(func() {
			var err error
			name, err = occam.RandomName()
			Expect(err).NotTo(HaveOccurred())
		})

		it.After(func() {
			Expect(docker.Container.Remove.Execute(container.ID)).To(Succeed())
			Expect(docker.Volume.Remove.Execute(occam.CacheVolumeNames(name))).To(Succeed())
			Expect(docker.Image.Remove.Execute(image.ID)).To(Succeed())
			Expect(os.RemoveAll(source)).To(Succeed())
		})

		it("builds and runs successfully", func() {
			var err error
			source, err = occam.Source(filepath.Join("testdata", "framework_dependent_executable"))
			Expect(err).NotTo(HaveOccurred())

			var logs fmt.Stringer
			image, logs, err = pack.Build.
				WithPullPolicy("never").
				WithBuildpacks(
					settings.Buildpacks.ICU.Online,
					settings.Buildpacks.DotnetCoreRuntime.Online,
					settings.Buildpacks.DotnetExecute.Online,
				).
				Execute(name, source)
			Expect(err).ToNot(HaveOccurred(), logs.String)

			container, err = docker.Container.Run.Execute(image.ID)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() string {
				logs, _ := docker.Container.Logs.Execute(container.ID)
				return logs.String()
			}).Should(Equal(`Setting ASPNETCORE_URLS=http://0.0.0.0:8080
Hello World!
`))

			Expect(logs).To(ContainLines(
				MatchRegexp(fmt.Sprintf(`%s \d+\.\d+\.\d+`, settings.BuildpackInfo.Buildpack.Name)),
				"  Assigning launch processes:",
				`    web (default): /workspace/MyApp`,
				"",
			))
		})

		context("when the app is a .NET 6 framework dependent executable", func() {
			it("builds and runs successfully", func() {
				var err error
				source, err = occam.Source(filepath.Join("testdata", "fde_6"))
				Expect(err).NotTo(HaveOccurred())

				var logs fmt.Stringer
				image, logs, err = pack.WithVerbose().Build.
					WithPullPolicy("never").
					WithBuildpacks(
						settings.Buildpacks.ICU.Online,
						settings.Buildpacks.DotnetCoreRuntime.Online,
						settings.Buildpacks.DotnetCoreASPNet.Online,
						settings.Buildpacks.DotnetExecute.Online,
					).
					Execute(name, source)
				Expect(err).ToNot(HaveOccurred(), logs.String)

				container, err = docker.Container.Run.
					WithEnv(map[string]string{"PORT": "8080"}).
					WithPublish("8080").
					WithPublishAll().
					Execute(image.ID)
				Expect(err).NotTo(HaveOccurred())

				Eventually(container).Should(Serve(ContainSubstring("fde_dotnet_6")).OnPort(8080))

				Expect(logs).To(ContainLines(
					MatchRegexp(fmt.Sprintf(`%s \d+\.\d+\.\d+`, settings.BuildpackInfo.Buildpack.Name)),
					"  Assigning launch processes:",
					`    web (default): /workspace/fde_dotnet_6`,
					"",
				))
			})
		})

		context("when BP_LIVE_RELOAD_ENABLED=true", func() {
			var noReloadContainer occam.Container

			it.After(func() {
				Expect(docker.Container.Remove.Execute(noReloadContainer.ID)).To(Succeed())
			})

			it("adds a default start process with watchexec and names the normal start process no-reload", func() {
				var err error
				source, err = occam.Source(filepath.Join("testdata", "framework_dependent_executable"))
				Expect(err).NotTo(HaveOccurred())

				var logs fmt.Stringer
				image, logs, err = pack.Build.
					WithPullPolicy("never").
					WithBuildpacks(
						settings.Buildpacks.ICU.Online,
						settings.Buildpacks.Watchexec.Online,
						settings.Buildpacks.DotnetCoreRuntime.Online,
						settings.Buildpacks.DotnetExecute.Online,
					).
					WithEnv(map[string]string{"BP_LIVE_RELOAD_ENABLED": "true"}).
					Execute(name, source)
				Expect(err).ToNot(HaveOccurred(), logs.String)

				container, err = docker.Container.Run.Execute(image.ID)
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() string {
					logs, _ := docker.Container.Logs.Execute(container.ID)
					return logs.String()
				}).Should(Equal(`Setting ASPNETCORE_URLS=http://0.0.0.0:8080
Hello World!
`))

				Expect(logs).To(ContainLines(
					MatchRegexp(fmt.Sprintf(`%s \d+\.\d+\.\d+`, settings.BuildpackInfo.Buildpack.Name)),
					"  Assigning launch processes:",
					`    web (default): watchexec --restart --watch /workspace --shell none -- /workspace/MyApp`,
					`    no-reload:     /workspace/MyApp`,
					"",
				))

				noReloadContainer, err = docker.Container.Run.WithEntrypoint("no-reload").Execute(image.ID)
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() string {
					logs, _ := docker.Container.Logs.Execute(noReloadContainer.ID)
					return logs.String()
				}).Should(Equal(`Setting ASPNETCORE_URLS=http://0.0.0.0:8080
Hello World!
`))
			})
		})
	})
}
