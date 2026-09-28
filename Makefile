LOCALBIN ?= $(shell pwd)/bin
ENVTEST ?= $(LOCALBIN)/setup-envtest
ENVTEST_K8S_VERSION = 1.35.0
KIND ?= $(LOCALBIN)/kind
CMCTL ?= $(LOCALBIN)/cmctl
SKAFFOLD ?= $(LOCALBIN)/skaffold
KUTTL ?= $(LOCALBIN)/kuttl
KUBECTL ?= $(LOCALBIN)/kubectl
GOARCH ?= $(shell go env GOARCH)
GOOS ?= $(shell go env GOOS)

all: test

generate:
	iidfile=$$(mktemp /tmp/iid-XXXXXX) && \
	docker build -f hack/Dockerfile --iidfile $$iidfile . && \
	docker run --rm -v $$PWD:/go/src/github.com/smartxworks/virtink -w /go/src/github.com/smartxworks/virtink $$(cat $$iidfile) ./hack/generate.sh && \
	rm -rf $$iidfile

fmt:
	go fmt ./...

test: envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test ./... -coverprofile cover.out

$(LOCALBIN):
	mkdir -p $(LOCALBIN)

.PHONY: envtest
envtest: $(ENVTEST)
$(ENVTEST): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: kind
kind: $(KIND)
$(KIND): $(LOCALBIN)
	curl -sLo $(KIND) https://kind.sigs.k8s.io/dl/v0.33.0/kind-$(GOOS)-$(GOARCH) && chmod +x $(KIND)

.PHONY: kubectl
kubectl: $(KUBECTL)
$(KUBECTL): $(LOCALBIN)
	curl -sLo $(KUBECTL) https://dl.k8s.io/release/v1.36.4/bin/$(GOOS)/$(GOARCH)/kubectl && chmod +x $(KUBECTL)

.PHONY: cmctl
cmctl: $(CMCTL)
$(CMCTL): $(LOCALBIN)
	curl -sLo cmctl.tar.gz https://github.com/cert-manager/cert-manager/releases/download/v1.8.2/cmctl-$(GOOS)-$(GOARCH).tar.gz
	tar xzf cmctl.tar.gz -C $(LOCALBIN)
	rm -rf cmctl.tar.gz

.PHONY: skaffold
skaffold: $(SKAFFOLD)
$(SKAFFOLD): $(LOCALBIN)
	curl -sLo $(SKAFFOLD) https://storage.googleapis.com/skaffold/releases/latest/skaffold-$(GOOS)-$(GOARCH) && chmod +x $(SKAFFOLD)

.PHONY: kuttl
kuttl: $(KUTTL)
$(KUTTL): $(LOCALBIN)
	curl -sLo $(KUTTL) https://github.com/kudobuilder/kuttl/releases/download/v0.12.1/kubectl-kuttl_0.12.1_$(GOOS)_$(shell uname -m) && chmod +x $(KUTTL)

# VM disk images served to CDI in e2e tests. They are downloaded rather than
# kept in git, and verified against test/e2e/images.sha256, which is also the
# key of their cache in the e2e workflow.
E2E_IMAGES_DIR := test/.e2e-images
E2E_UBUNTU_IMAGE_URL := https://cloud-images.ubuntu.com/releases/jammy/release-20260913/ubuntu-22.04-server-cloudimg-amd64.img

.PHONY: e2e-images
e2e-images:
	mkdir -p $(E2E_IMAGES_DIR)
	cd $(E2E_IMAGES_DIR) && sha256sum --quiet -c ../e2e/images.sha256 >/dev/null 2>&1 || { \
		curl -fsSLo ubuntu-22.04-server-cloudimg-amd64.img $(E2E_UBUNTU_IMAGE_URL) && \
		sha256sum --quiet -c ../e2e/images.sha256; }

E2E_KIND_CLUSTER_NAME := virtink-e2e-$(shell date "+%Y-%m-%d-%H-%M-%S")
E2E_KIND_CLUSTER_KUBECONFIG := /tmp/$(E2E_KIND_CLUSTER_NAME).kubeconfig

.PHONY: e2e-image
e2e-image:
	docker buildx build -t virt-controller:e2e -f build/virt-controller/Dockerfile --build-arg PRERUNNER_IMAGE=virt-prerunner:e2e --load .
	docker buildx build -t virt-daemon:e2e -f build/virt-daemon/Dockerfile --load .
	docker buildx build -t virt-prerunner:e2e -f build/virt-prerunner/Dockerfile  --load .
	docker buildx build -t virtink-image-rootfs-ubuntu:e2e -f samples/Dockerfile.image-rootfs-ubuntu --load .

e2e: kind kubectl cmctl skaffold kuttl e2e-image e2e-images
	echo "e2e kind cluster: $(E2E_KIND_CLUSTER_NAME)"

	$(KIND) create cluster --config test/e2e/config/kind/config.yaml --name $(E2E_KIND_CLUSTER_NAME) --kubeconfig $(E2E_KIND_CLUSTER_KUBECONFIG)
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) virt-controller:e2e
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) virt-daemon:e2e
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) virt-prerunner:e2e
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) virtink-image-rootfs-ubuntu:e2e

	docker pull docker.io/calico/cni:v3.23.5
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) docker.io/calico/cni:v3.23.5
	docker pull docker.io/calico/node:v3.23.5
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) docker.io/calico/node:v3.23.5
	docker pull docker.io/calico/kube-controllers:v3.23.5
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) docker.io/calico/kube-controllers:v3.23.5
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) apply -f https://projectcalico.docs.tigera.io/archive/v3.23/manifests/calico.yaml
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) wait -n kube-system deployment calico-kube-controllers --for condition=Available --timeout -1s

	docker pull quay.io/jetstack/cert-manager-controller:v1.8.2
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) quay.io/jetstack/cert-manager-controller:v1.8.2
	docker pull quay.io/jetstack/cert-manager-cainjector:v1.8.2
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) quay.io/jetstack/cert-manager-cainjector:v1.8.2
	docker pull quay.io/jetstack/cert-manager-webhook:v1.8.2
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) quay.io/jetstack/cert-manager-webhook:v1.8.2
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.8.2/cert-manager.yaml
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(CMCTL) check api --wait=10m

	docker pull quay.io/kubevirt/cdi-operator:v1.53.0
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) quay.io/kubevirt/cdi-operator:v1.53.0
	docker pull quay.io/kubevirt/cdi-apiserver:v1.53.0
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) quay.io/kubevirt/cdi-apiserver:v1.53.0
	docker pull  quay.io/kubevirt/cdi-controller:v1.53.0
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) quay.io/kubevirt/cdi-controller:v1.53.0
	docker pull quay.io/kubevirt/cdi-uploadproxy:v1.53.0
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) quay.io/kubevirt/cdi-uploadproxy:v1.53.0
	docker pull quay.io/kubevirt/cdi-importer:v1.53.0
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) quay.io/kubevirt/cdi-importer:v1.53.0
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) apply -f https://github.com/kubevirt/containerized-data-importer/releases/download/v1.53.0/cdi-operator.yaml
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) wait -n cdi deployment cdi-operator --for condition=Available --timeout -1s
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) apply -f https://github.com/kubevirt/containerized-data-importer/releases/download/v1.53.0/cdi-cr.yaml
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) wait cdi.cdi.kubevirt.io cdi --for condition=Available --timeout -1s
# Dirty page cache written to NFS exceeds the default importer memory limit (600M) and gets it OOM killed.
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) patch cdi cdi --type merge -p '{"spec":{"config":{"podResourceRequirements":{"requests":{"cpu":"100m","memory":"60M"},"limits":{"cpu":"1","memory":"2Gi"}}}}}'

	docker pull itsthenetwork/nfs-server-alpine:12
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) itsthenetwork/nfs-server-alpine:12
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) apply -f test/e2e/config/nfs/nfs-server.yaml
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) wait -n nfs deployment nfs-server --for condition=Available --timeout -1s
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) apply \
		-f https://raw.githubusercontent.com/kubernetes-csi/csi-driver-nfs/v4.13.4/deploy/v4.13.4/rbac-csi-nfs.yaml \
		-f https://raw.githubusercontent.com/kubernetes-csi/csi-driver-nfs/v4.13.4/deploy/v4.13.4/csi-nfs-driverinfo.yaml \
		-f https://raw.githubusercontent.com/kubernetes-csi/csi-driver-nfs/v4.13.4/deploy/v4.13.4/csi-nfs-controller.yaml \
		-f https://raw.githubusercontent.com/kubernetes-csi/csi-driver-nfs/v4.13.4/deploy/v4.13.4/csi-nfs-node.yaml
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) rollout status -n kube-system deployment csi-nfs-controller --timeout 10m
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) rollout status -n kube-system daemonset csi-nfs-node --timeout 10m
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) apply -f test/e2e/config/nfs/storageclass.yaml

	docker pull nginx:1.29-alpine
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) nginx:1.29-alpine
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) apply -f test/e2e/config/images/images.yaml
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) wait -n e2e-images deployment images --for condition=Available --timeout 5m

	PATH=$(LOCALBIN):$(PATH) $(SKAFFOLD) render --offline=true --default-repo="" --digest-source=tag --images virt-controller:e2e,virt-daemon:e2e | KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) apply -f -
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) wait -n virtink-system deployment virt-controller --for condition=Available --timeout -1s

	docker pull smartxworks/virtink-kernel-5.15.12
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) smartxworks/virtink-kernel-5.15.12
	docker pull smartxworks/virtink-container-disk-ubuntu
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) smartxworks/virtink-container-disk-ubuntu
	docker pull smartxworks/virtink-container-rootfs-ubuntu
	$(KIND) load docker-image --name $(E2E_KIND_CLUSTER_NAME) smartxworks/virtink-container-rootfs-ubuntu
	PATH=$(LOCALBIN):$(PATH) KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUTTL) test --config test/e2e/kuttl-test.yaml

# kuttl does not wait for test namespaces to be deleted. Wait for them and their NFS volumes, and unmount NFS
# volumes left on nodes while the in-cluster NFS server is still running, otherwise the kernel NFS client
# hangs and the nodes can not be removed.
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) get namespace -o name | grep '^namespace/kuttl-test-' | xargs -r env KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) wait --for=delete --timeout 10m
	KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) get pv -o name | xargs -r env KUBECONFIG=$(E2E_KIND_CLUSTER_KUBECONFIG) $(KUBECTL) wait --for=delete --timeout 10m
	for node in $$($(KIND) get nodes --name $(E2E_KIND_CLUSTER_NAME)); do \
		docker exec $$node sh -c "grep -E ' nfs4? ' /proc/mounts | cut -d' ' -f2 | xargs -r umount -l"; \
	done
	$(KIND) delete cluster --name $(E2E_KIND_CLUSTER_NAME)
