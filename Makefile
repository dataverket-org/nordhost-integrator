GO ?= $(shell which go)
OS ?= $(shell $(GO) env GOOS)
ARCH ?= $(shell $(GO) env GOARCH)

# Must be absolute: `go test ./...` runs each package with that package's
# own directory as CWD, so a relative path only resolves correctly for the
# root package's tests. Computed separately from any $CONFIG already in the
# environment (e.g. a shell profile export used for running cmd/main
# directly), since that's commonly set to a relative path and Make's `?=`
# won't override an inherited env var.
TEST_CONFIG ?= $(shell pwd)/config.json

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p "$(LOCALBIN)"

## Tool Binaries
ENVTEST ?= $(LOCALBIN)/setup-envtest
ENVTEST_VERSION ?= release-0.22

# Highest Kubernetes version currently published for envtest at time of
# writing; our k8s.io/* deps track a newer minor (0.37) than envtest has
# published binaries for, but envtest's bare control plane (Namespace CRUD
# only, no CRDs) works fine against an older apiserver for this purpose.
ENVTEST_K8S_VERSION ?= 1.33.0

.PHONY: test
test: setup-envtest
	TEST_ASSET_ETCD=$(LOCALBIN)/k8s/$(ENVTEST_K8S_VERSION)-$(OS)-$(ARCH)/etcd \
	TEST_ASSET_KUBE_APISERVER=$(LOCALBIN)/k8s/$(ENVTEST_K8S_VERSION)-$(OS)-$(ARCH)/kube-apiserver \
	TEST_ASSET_KUBECTL=$(LOCALBIN)/k8s/$(ENVTEST_K8S_VERSION)-$(OS)-$(ARCH)/kubectl \
	CONFIG=$(TEST_CONFIG) \
	$(GO) test -v ./...

.PHONY: clean
clean:
	chmod -R u+w $(LOCALBIN) 2>/dev/null || true
	rm -rf $(LOCALBIN)

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@"$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] && [ "$$(readlink -- "$(1)" 2>/dev/null)" = "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f "$(1)" ;\
GOBIN="$(LOCALBIN)" go install $${package} ;\
mv "$(LOCALBIN)/$$(basename "$(1)")" "$(1)-$(3)" ;\
} ;\
ln -sf "$$(realpath "$(1)-$(3)")" "$(1)"
endef
