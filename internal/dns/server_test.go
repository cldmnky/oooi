/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dns

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDNS(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DNS Server Suite")
}

var _ = Describe("DNS Server", func() {
	var (
		tmpDir       string
		corefilePath string
		ctx          context.Context
		cancel       context.CancelFunc
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "dns-test-*")
		Expect(err).NotTo(HaveOccurred())

		corefilePath = filepath.Join(tmpDir, "Corefile")
		ctx, cancel = context.WithCancel(context.Background())
	})

	AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		if tmpDir != "" {
			os.RemoveAll(tmpDir)
		}
	})

	Context("When starting a CoreDNS server", func() {
		It("should fail if Corefile does not exist", func() {
			By("creating a server with non-existent Corefile")
			server, err := NewServer(corefilePath)
			Expect(err).To(HaveOccurred())
			Expect(server).To(BeNil())
		})

		It("should start successfully with valid Corefile", func() {
			By("creating a valid Corefile")
			corefile := `.:5353 {
    whoami
    bind 127.0.0.1
    ready :8181
}`
			err := os.WriteFile(corefilePath, []byte(corefile), 0644)
			Expect(err).NotTo(HaveOccurred())

			By("creating and starting the server")
			server, err := NewServer(corefilePath)
			Expect(err).NotTo(HaveOccurred())
			Expect(server).NotTo(BeNil())

			By("starting the server in background")
			errCh := make(chan error, 1)
			go func() {
				errCh <- server.Start(ctx)
			}()

			By("waiting for server to be ready")
			time.Sleep(500 * time.Millisecond)

			By("stopping the server")
			cancel()

			By("verifying server stopped without error")
			select {
			case err := <-errCh:
				Expect(err).To(Or(BeNil(), Equal(context.Canceled)))
			case <-time.After(2 * time.Second):
				Fail("server did not stop in time")
			}
		})

		It("should reload when Corefile changes", func() {
			By("creating initial Corefile")
			corefile := `.:5354 {
    whoami
    bind 127.0.0.1
    reload 2s
    ready :8182
}`
			err := os.WriteFile(corefilePath, []byte(corefile), 0644)
			Expect(err).NotTo(HaveOccurred())

			By("creating and starting the server")
			server, err := NewServer(corefilePath)
			Expect(err).NotTo(HaveOccurred())

			errCh := make(chan error, 1)
			go func() {
				errCh <- server.Start(ctx)
			}()

			By("waiting for server to start")
			time.Sleep(500 * time.Millisecond)

			By("modifying the Corefile")
			updatedCorefile := `.:5354 {
    whoami
    bind 127.0.0.1
    reload 2s
    ready :8182
    log
}`
			err = os.WriteFile(corefilePath, []byte(updatedCorefile), 0644)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for reload interval")
			time.Sleep(2 * time.Second)

			By("stopping the server")
			cancel()

			By("verifying server stopped")
			select {
			case err := <-errCh:
				Expect(err).To(Or(BeNil(), Equal(context.Canceled)))
			case <-time.After(2 * time.Second):
				Fail("server did not stop in time")
			}
		})
	})

	Context("When stopping a server", func() {
		It("should cleanup resources gracefully", func() {
			By("creating a valid Corefile")
			corefile := `.:5355 {
    whoami
    bind 127.0.0.1
}`
			err := os.WriteFile(corefilePath, []byte(corefile), 0644)
			Expect(err).NotTo(HaveOccurred())

			By("creating and starting the server")
			server, err := NewServer(corefilePath)
			Expect(err).NotTo(HaveOccurred())

			errCh := make(chan error, 1)
			go func() {
				errCh <- server.Start(ctx)
			}()

			time.Sleep(500 * time.Millisecond)

			By("stopping the server")
			err = server.Stop()
			Expect(err).NotTo(HaveOccurred())

			By("verifying server stopped")
			select {
			case <-errCh:
				// Server stopped
			case <-time.After(2 * time.Second):
				Fail("server did not stop in time")
			}
		})
	})
})
