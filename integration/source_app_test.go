package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paketo-buildpacks/occam"
	"github.com/sclevine/spec"

	. "github.com/onsi/gomega"
	. "github.com/paketo-buildpacks/occam/matchers"
)

func testSourceApp(t *testing.T, context spec.G, it spec.S) {
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

	context("when building a default source app", func() {
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

		if !strings.Contains(builder.Local.Stack.ID, "jammy") {
			it("builds and runs a .NET 3.1 app successfully", func() {
				var err error
				source, err = occam.Source(filepath.Join("testdata", "source_app"))
				Expect(err).NotTo(HaveOccurred())

				var logs fmt.Stringer
				image, logs, err = pack.Build.
					WithPullPolicy("never").
					WithBuildpacks(
						settings.Buildpacks.ICU.Online,
						settings.Buildpacks.DotnetCoreRuntime.Online,
						settings.Buildpacks.DotnetCoreASPNet.Online,
						settings.Buildpacks.DotnetCoreSDK.Online,
						settings.Buildpacks.DotnetPublish.Online,
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

				Eventually(container).Should(Serve(ContainSubstring("simple_3_0_app")).OnPort(8080))
			})
		}

		context("when 'net6.0' is specified as the TargetFramework", func() {
			it("builds and runs successfully", func() {
				var err error
				source, err = occam.Source(filepath.Join("testdata", "source_6"))
				Expect(err).NotTo(HaveOccurred())

				var logs fmt.Stringer
				image, logs, err = pack.Build.
					WithPullPolicy("never").
					WithBuildpacks(
						settings.Buildpacks.ICU.Online,
						settings.Buildpacks.DotnetCoreRuntime.Online,
						settings.Buildpacks.DotnetCoreASPNet.Online,
						settings.Buildpacks.DotnetCoreSDK.Online,
						settings.Buildpacks.DotnetPublish.Online,
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

				Eventually(container).Should(Serve(ContainSubstring("source_6_app")).OnPort(8080))
			})

			context("remote debugging is enabled", func() {
				var vsdbgContainer occam.Container
				it.After(func() {
					Expect(docker.Container.Remove.Execute(vsdbgContainer.ID)).To(Succeed())
				})
				it("builds and runs successfully", func() {
					var err error
					source, err = occam.Source(filepath.Join("testdata", "source_6"))
					Expect(err).NotTo(HaveOccurred())

					var logs fmt.Stringer
					image, logs, err = pack.Build.
						WithPullPolicy("never").
						WithBuildpacks(
							settings.Buildpacks.ICU.Online,
							settings.Buildpacks.Vsdbg.Online,
							settings.Buildpacks.DotnetCoreRuntime.Online,
							settings.Buildpacks.DotnetCoreASPNet.Online,
							settings.Buildpacks.DotnetCoreSDK.Online,
							settings.Buildpacks.DotnetPublish.Online,
							settings.Buildpacks.DotnetExecute.Online,
						).
						WithEnv(map[string]string{
							"BP_DEBUG_ENABLED": "true",
						}).
						Execute(name, source)
					Expect(err).ToNot(HaveOccurred(), logs.String)

					container, err = docker.Container.Run.
						WithEnv(map[string]string{"PORT": "8080"}).
						WithPublish("8080").
						WithPublishAll().
						Execute(image.ID)
					Expect(err).NotTo(HaveOccurred())

					Eventually(container).Should(Serve(ContainSubstring("source_6_app")).OnPort(8080))

					vsdbgContainer, err = docker.Container.Run.
						WithEntrypoint("launcher").
						WithCommand("vsdbg --help").
						Execute(image.ID)
					Expect(err).NotTo(HaveOccurred())

					Eventually(func() string {
						cLogs, err := docker.Container.Logs.Execute(vsdbgContainer.ID)
						Expect(err).NotTo(HaveOccurred())
						return cLogs.String()
					}).Should(ContainSubstring(`Microsoft .NET Core Debugger (vsdbg)`))
				})
			})
		})
	})
}
